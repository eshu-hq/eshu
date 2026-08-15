// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scan

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Result is the canonical scan result envelope. Status is one of "failed",
// "submitted", "partial", or "ready"; it starts at "failed" so an early return
// never reads as success.
type Result struct {
	Command      string         `json:"command"`
	Status       string         `json:"status"`
	Target       Target         `json:"target"`
	Timings      Timings        `json:"timings"`
	Evidence     Evidence       `json:"evidence"`
	StatusReport PipelineStatus `json:"status_report"`
	QueryProbe   map[string]any `json:"query_probe,omitempty"`
	// Truth is the envelope's truth block, rendered as a sibling of data by
	// the CLI wrapper rather than nested inside the result.
	Truth    map[string]any `json:"-"`
	Warnings []string       `json:"warnings,omitempty"`
}

// Timings are the scan's measured milestones. The pointer fields are null
// rather than zero when the milestone was not measured, so a missing
// measurement is never reported as an instantaneous one.
type Timings struct {
	BootstrapCompleteMS  int64  `json:"bootstrap_complete_ms"`
	CollectorCompleteMS  *int64 `json:"collector_complete_ms"`
	ProjectionCompleteMS *int64 `json:"projection_complete_ms"`
	QueueZeroMS          *int64 `json:"queue_zero_ms"`
	ReadinessWaitMS      *int64 `json:"readiness_wait_ms"`
}

// Evidence records where a scan's claims came from: the binary it ran and the
// endpoints it read.
type Evidence struct {
	BootstrapBinary string `json:"bootstrap_binary"`
	ServiceURL      string `json:"service_url"`
	StatusEndpoint  string `json:"status_endpoint"`
	QueryEndpoint   string `json:"query_endpoint"`
}

// PipelineStatus is the subset of /api/v0/status/pipeline the scan family
// reads. Fields absent from the response decode to their zero value, which
// EvaluateReadiness treats as "nothing outstanding" -- an empty history is
// caught separately rather than read as drained.
type PipelineStatus struct {
	Version           string            `json:"version,omitempty"`
	AsOf              string            `json:"as_of,omitempty"`
	Health            Health            `json:"health,omitempty"`
	Queue             Queue             `json:"queue,omitempty"`
	GenerationHistory GenerationHistory `json:"generation_history,omitempty"`
	StageSummaries    []StageSummary    `json:"stage_summaries,omitempty"`
	DomainBacklogs    []DomainBacklog   `json:"domain_backlogs,omitempty"`
	ScopeActivity     ScopeActivity     `json:"scope_activity,omitempty"`
}

// Health is the pipeline's reported health state and the reasons behind it.
type Health struct {
	State   string   `json:"state,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

// Queue is the reducer queue's aggregate counters.
type Queue struct {
	Total                 int     `json:"total,omitempty"`
	OverdueClaims         int     `json:"overdue_claims,omitempty"`
	OldestOutstandingAgeS float64 `json:"oldest_outstanding_age,omitempty"`
	Outstanding           int     `json:"outstanding,omitempty"`
	Pending               int     `json:"pending,omitempty"`
	InFlight              int     `json:"in_flight,omitempty"`
	Retrying              int     `json:"retrying,omitempty"`
	Succeeded             int     `json:"succeeded,omitempty"`
	Failed                int     `json:"failed,omitempty"`
	DeadLetter            int     `json:"dead_letter,omitempty"`
}

// GenerationHistory counts scope generations by lifecycle state.
type GenerationHistory struct {
	Active     int `json:"active,omitempty"`
	Pending    int `json:"pending,omitempty"`
	Completed  int `json:"completed,omitempty"`
	Superseded int `json:"superseded,omitempty"`
	Failed     int `json:"failed,omitempty"`
	Other      int `json:"other,omitempty"`
}

// StageSummary is one pipeline stage's work-item counters.
type StageSummary struct {
	Stage      string `json:"stage,omitempty"`
	Pending    int    `json:"pending,omitempty"`
	Claimed    int    `json:"claimed,omitempty"`
	Running    int    `json:"running,omitempty"`
	Retrying   int    `json:"retrying,omitempty"`
	Succeeded  int    `json:"succeeded,omitempty"`
	Failed     int    `json:"failed,omitempty"`
	DeadLetter int    `json:"dead_letter,omitempty"`
}

// DomainBacklog is one reducer domain's outstanding work.
type DomainBacklog struct {
	Domain      string  `json:"domain,omitempty"`
	Outstanding int     `json:"outstanding,omitempty"`
	InFlight    int     `json:"in_flight,omitempty"`
	Blocked     int     `json:"blocked,omitempty"`
	Retrying    int     `json:"retrying,omitempty"`
	Failed      int     `json:"failed,omitempty"`
	DeadLetter  int     `json:"dead_letter,omitempty"`
	OldestAgeS  float64 `json:"oldest_age,omitempty"`
}

// ScopeActivity counts scopes by what the last collection run saw.
type ScopeActivity struct {
	Active    int `json:"active,omitempty"`
	Changed   int `json:"changed,omitempty"`
	Unchanged int `json:"unchanged,omitempty"`
}

// ReadinessVerdict is one readiness evaluation. Ready and Terminal are
// mutually exclusive: Terminal means stop and report the failure, a verdict
// that is neither means poll again, and Reason always explains which.
type ReadinessVerdict struct {
	Ready    bool
	Terminal bool
	Reason   string
}

// waitForReadiness polls the pipeline until it is drained and healthy, a
// terminal failure appears, the deadline passes, or the context ends. The
// deadline is measured from scanStartedAt, so the bootstrap child's runtime
// counts against the scan's timeout rather than resetting it.
func waitForReadiness(
	ctx context.Context,
	rt Runtime,
	opts Options,
	result Result,
	scanStartedAt time.Time,
	bootstrapCompletedAt time.Time,
) (Result, error) {
	deadline := scanStartedAt.Add(opts.Timeout)
	for {
		if err := ctx.Err(); err != nil {
			return result, err //nolint:wrapcheck // callers match on context.Canceled/DeadlineExceeded
		}
		status, err := rt.FetchStatus(rt.Client)
		if err != nil {
			return result, fmt.Errorf("scan readiness status check: %w", err)
		}
		result.StatusReport = status
		verdict := EvaluateReadiness(status)
		now := rt.Now()
		if verdict.Ready {
			queueZeroMS := durationMillis(now.Sub(scanStartedAt))
			readinessWaitMS := durationMillis(now.Sub(bootstrapCompletedAt))
			result.Timings.QueueZeroMS = &queueZeroMS
			result.Timings.ReadinessWaitMS = &readinessWaitMS
			return result, nil
		}
		if verdict.Terminal {
			return result, fmt.Errorf("%s", verdict.Reason)
		}
		if !now.Before(deadline) {
			return result, fmt.Errorf("scan readiness timed out: %s", verdict.Reason)
		}
		if err := rt.Wait(ctx, opts.PollInterval); err != nil {
			return result, err //nolint:wrapcheck // Wait returns the caller's own context error
		}
	}
}

// EvaluateReadiness decides whether an indexed source is queryable from one
// pipeline status report. Readiness means the queue is drained, no stage or
// domain holds failed or dead-letter work, a generation completed or is
// active, and health reads healthy. Process health alone is never readiness,
// and a report with no generation history is not-ready rather than drained.
func EvaluateReadiness(status PipelineStatus) ReadinessVerdict {
	if status.Queue.DeadLetter > 0 {
		return ReadinessVerdict{Terminal: true, Reason: "queue has dead-letter work"}
	}
	if status.Queue.Failed > 0 {
		return ReadinessVerdict{Terminal: true, Reason: "queue has failed work"}
	}
	for _, stage := range status.StageSummaries {
		if stage.DeadLetter > 0 || stage.Failed > 0 {
			return ReadinessVerdict{Terminal: true, Reason: fmt.Sprintf("stage %s has failed or dead-letter work", stage.Stage)}
		}
	}
	for _, domain := range status.DomainBacklogs {
		if domain.DeadLetter > 0 || domain.Failed > 0 {
			return ReadinessVerdict{Terminal: true, Reason: fmt.Sprintf("domain %s has failed or dead-letter work", domain.Domain)}
		}
	}
	if status.GenerationHistory.Failed > 0 {
		return ReadinessVerdict{Terminal: true, Reason: "generation history has failed generations"}
	}
	switch strings.ToLower(strings.TrimSpace(status.Health.State)) {
	case "degraded":
		return ReadinessVerdict{Terminal: true, Reason: strings.Join(status.Health.Reasons, "; ")}
	case "stalled":
		return ReadinessVerdict{Terminal: true, Reason: strings.Join(status.Health.Reasons, "; ")}
	}
	if status.Queue.Outstanding > 0 || status.Queue.Pending > 0 || status.Queue.InFlight > 0 || status.Queue.Retrying > 0 {
		return ReadinessVerdict{Reason: "queue still has outstanding work"}
	}
	if status.GenerationHistory.Pending > 0 {
		return ReadinessVerdict{Reason: "generations are still pending"}
	}
	if status.GenerationHistory.Completed == 0 && status.GenerationHistory.Active == 0 {
		return ReadinessVerdict{Reason: "no completed or active generation observed"}
	}
	if strings.EqualFold(strings.TrimSpace(status.Health.State), "healthy") {
		return ReadinessVerdict{Ready: true, Reason: "pipeline healthy and drained"}
	}
	return ReadinessVerdict{Reason: "pipeline is not healthy yet"}
}
