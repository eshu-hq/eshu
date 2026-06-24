// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type scanResult struct {
	Command      string             `json:"command"`
	Status       string             `json:"status"`
	Target       scanTarget         `json:"target"`
	Timings      scanTimings        `json:"timings"`
	Evidence     scanEvidence       `json:"evidence"`
	StatusReport scanPipelineStatus `json:"status_report"`
	QueryProbe   map[string]any     `json:"query_probe,omitempty"`
	Truth        map[string]any     `json:"-"`
	Warnings     []string           `json:"warnings,omitempty"`
}

type scanTimings struct {
	BootstrapCompleteMS  int64  `json:"bootstrap_complete_ms"`
	CollectorCompleteMS  *int64 `json:"collector_complete_ms"`
	ProjectionCompleteMS *int64 `json:"projection_complete_ms"`
	QueueZeroMS          *int64 `json:"queue_zero_ms"`
	ReadinessWaitMS      *int64 `json:"readiness_wait_ms"`
}

type scanEvidence struct {
	BootstrapBinary string `json:"bootstrap_binary"`
	ServiceURL      string `json:"service_url"`
	StatusEndpoint  string `json:"status_endpoint"`
	QueryEndpoint   string `json:"query_endpoint"`
}

type scanPipelineStatus struct {
	Version           string                `json:"version,omitempty"`
	AsOf              string                `json:"as_of,omitempty"`
	Health            scanHealth            `json:"health,omitempty"`
	Queue             scanQueue             `json:"queue,omitempty"`
	GenerationHistory scanGenerationHistory `json:"generation_history,omitempty"`
	StageSummaries    []scanStageSummary    `json:"stage_summaries,omitempty"`
	DomainBacklogs    []scanDomainBacklog   `json:"domain_backlogs,omitempty"`
	ScopeActivity     scanScopeActivity     `json:"scope_activity,omitempty"`
}

type scanHealth struct {
	State   string   `json:"state,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

type scanQueue struct {
	Outstanding int `json:"outstanding,omitempty"`
	Pending     int `json:"pending,omitempty"`
	InFlight    int `json:"in_flight,omitempty"`
	Retrying    int `json:"retrying,omitempty"`
	Succeeded   int `json:"succeeded,omitempty"`
	Failed      int `json:"failed,omitempty"`
	DeadLetter  int `json:"dead_letter,omitempty"`
}

type scanGenerationHistory struct {
	Active    int `json:"active,omitempty"`
	Pending   int `json:"pending,omitempty"`
	Completed int `json:"completed,omitempty"`
	Failed    int `json:"failed,omitempty"`
}

type scanStageSummary struct {
	Stage      string `json:"stage,omitempty"`
	Pending    int    `json:"pending,omitempty"`
	Claimed    int    `json:"claimed,omitempty"`
	Running    int    `json:"running,omitempty"`
	Retrying   int    `json:"retrying,omitempty"`
	Failed     int    `json:"failed,omitempty"`
	DeadLetter int    `json:"dead_letter,omitempty"`
}

type scanDomainBacklog struct {
	Domain      string `json:"domain,omitempty"`
	Outstanding int    `json:"outstanding,omitempty"`
	Retrying    int    `json:"retrying,omitempty"`
	Failed      int    `json:"failed,omitempty"`
	DeadLetter  int    `json:"dead_letter,omitempty"`
}

type scanScopeActivity struct {
	Active    int `json:"active,omitempty"`
	Changed   int `json:"changed,omitempty"`
	Unchanged int `json:"unchanged,omitempty"`
}

func waitForScanReadiness(
	ctx context.Context,
	client *APIClient,
	opts scanOptions,
	result scanResult,
	scanStartedAt time.Time,
	bootstrapCompletedAt time.Time,
) (scanResult, error) {
	deadline := scanStartedAt.Add(opts.Timeout)
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		status, err := scanFetchPipelineStatus(client)
		if err != nil {
			return result, fmt.Errorf("scan readiness status check: %w", err)
		}
		result.StatusReport = status
		verdict := evaluateScanReadiness(status)
		now := scanNow()
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
		if err := scanWait(ctx, opts.PollInterval); err != nil {
			return result, err
		}
	}
}

type scanReadinessVerdict struct {
	Ready    bool
	Terminal bool
	Reason   string
}

func evaluateScanReadiness(status scanPipelineStatus) scanReadinessVerdict {
	if status.Queue.DeadLetter > 0 {
		return scanReadinessVerdict{Terminal: true, Reason: "queue has dead-letter work"}
	}
	if status.Queue.Failed > 0 {
		return scanReadinessVerdict{Terminal: true, Reason: "queue has failed work"}
	}
	for _, stage := range status.StageSummaries {
		if stage.DeadLetter > 0 || stage.Failed > 0 {
			return scanReadinessVerdict{Terminal: true, Reason: fmt.Sprintf("stage %s has failed or dead-letter work", stage.Stage)}
		}
	}
	for _, domain := range status.DomainBacklogs {
		if domain.DeadLetter > 0 || domain.Failed > 0 {
			return scanReadinessVerdict{Terminal: true, Reason: fmt.Sprintf("domain %s has failed or dead-letter work", domain.Domain)}
		}
	}
	if status.GenerationHistory.Failed > 0 {
		return scanReadinessVerdict{Terminal: true, Reason: "generation history has failed generations"}
	}
	switch strings.ToLower(strings.TrimSpace(status.Health.State)) {
	case "degraded":
		return scanReadinessVerdict{Terminal: true, Reason: strings.Join(status.Health.Reasons, "; ")}
	case "stalled":
		return scanReadinessVerdict{Terminal: true, Reason: strings.Join(status.Health.Reasons, "; ")}
	}
	if status.Queue.Outstanding > 0 || status.Queue.Pending > 0 || status.Queue.InFlight > 0 || status.Queue.Retrying > 0 {
		return scanReadinessVerdict{Reason: "queue still has outstanding work"}
	}
	if status.GenerationHistory.Pending > 0 {
		return scanReadinessVerdict{Reason: "generations are still pending"}
	}
	if status.GenerationHistory.Completed == 0 && status.GenerationHistory.Active == 0 {
		return scanReadinessVerdict{Reason: "no completed or active generation observed"}
	}
	if strings.EqualFold(strings.TrimSpace(status.Health.State), "healthy") {
		return scanReadinessVerdict{Ready: true, Reason: "pipeline healthy and drained"}
	}
	return scanReadinessVerdict{Reason: "pipeline is not healthy yet"}
}
