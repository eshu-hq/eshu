// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Claim-plane read/report surface (#8875, #9376).
//
// These five routes complete the 20-route workflow.v1 table on the in-tree
// client: the read plane GetRunStatus, ListRuns, GetRunCompleteness,
// QueueDepthSnapshot and the report plane ReportRunProgress. The satellite
// has served them since eshu-workflow#17; no in-tree caller uses them yet,
// so they travel the shared post path (and its major-1 pin) with compat
// tests proving the stub bodies decode into the in-tree types. Only
// TestReadReportPlaneLiveCompat proves the live wire shapes, against the
// bound satellite build when the satellite leg is configured. The wire shapes
// below mirror the protocol table field-for-field but are declared locally,
// like the claim and control paths: this package takes no dependency on the
// satellite module.

// RunStatusReport is the read-plane status of one run: the run, per-family
// published progress, and completeness checkpoints.
type RunStatusReport struct {
	Run          Run
	Progress     []CollectorRunProgress
	Completeness []CompletenessState
	AsOf         time.Time
}

// RunPage is one page of run summaries. NextCursor is empty on the last page.
type RunPage struct {
	Runs       []Run
	NextCursor string
	AsOf       time.Time
}

// RunCompleteness carries the completeness checkpoints of one run.
type RunCompleteness struct {
	RunID  string
	States []CompletenessState
	AsOf   time.Time
}

// FamilyQueueDepth is the pending/claimed depth of one collector family.
type FamilyQueueDepth struct {
	CollectorKind scope.CollectorKind
	Pending       int64
	Claimed       int64
}

// QueueDepthSnapshot is the read-plane queue-depth observation that replaces
// direct work-item table reads for backpressure status.
type QueueDepthSnapshot struct {
	Families      []FamilyQueueDepth
	GlobalPending int64
	GlobalClaimed int64
	AsOf          time.Time
}

// claimPlaneWireProgress mirrors one protocol collector progress row. Phase
// count maps travel keyed by "keyspace/phase".
type claimPlaneWireProgress struct {
	CollectorKind            string         `json:"collector_kind"`
	TotalWorkItems           int            `json:"total_work_items"`
	PendingWorkItems         int            `json:"pending_work_items"`
	ClaimedWorkItems         int            `json:"claimed_work_items"`
	CompletedWorkItems       int            `json:"completed_work_items"`
	FailedTerminalItems      int            `json:"failed_terminal_items"`
	FactlessCompletedItems   int            `json:"factless_completed_items"`
	PublishedPhaseCounts     map[string]int `json:"published_phase_counts,omitempty"`
	TerminalDeadLetterCounts map[string]int `json:"terminal_dead_letter_counts,omitempty"`
}

// wireProgress converts one in-tree progress row to its protocol shape.
func wireProgress(progress CollectorRunProgress) claimPlaneWireProgress {
	return claimPlaneWireProgress{
		CollectorKind:            string(progress.CollectorKind),
		TotalWorkItems:           progress.TotalWorkItems,
		PendingWorkItems:         progress.PendingWorkItems,
		ClaimedWorkItems:         progress.ClaimedWorkItems,
		CompletedWorkItems:       progress.CompletedWorkItems,
		FailedTerminalItems:      progress.FailedTerminalItems,
		FactlessCompletedItems:   progress.FactlessCompletedItems,
		PublishedPhaseCounts:     wirePhaseCounts(progress.PublishedPhaseCounts),
		TerminalDeadLetterCounts: wirePhaseCounts(progress.TerminalDeadLetterCounts),
	}
}

// wirePhaseCounts keys one phase-count map by "keyspace/phase".
func wirePhaseCounts(counts map[PhasePublicationKey]int) map[string]int {
	if len(counts) == 0 {
		return nil
	}
	wire := make(map[string]int, len(counts))
	for key, count := range counts {
		wire[string(key.Keyspace)+"/"+string(key.PhaseName)] = count
	}
	return wire
}

// wirePhaseCountsToCounts splits one protocol phase-count map back into
// publication keys. A key without the "keyspace/phase" separator is a decode
// error, never a silent misattribution.
func wirePhaseCountsToCounts(wire map[string]int) (map[PhasePublicationKey]int, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	counts := make(map[PhasePublicationKey]int, len(wire))
	for key, count := range wire {
		keyspace, phase, ok := strings.Cut(key, "/")
		if !ok || keyspace == "" || phase == "" {
			return nil, fmt.Errorf("claim-plane phase count key %q is not keyspace/phase", key)
		}
		counts[PhasePublicationKey{
			Keyspace:  gpphase.Keyspace(keyspace),
			PhaseName: gpphase.Phase(phase),
		}] = count
	}
	return counts, nil
}

func wireProgressToProgress(wire claimPlaneWireProgress) (CollectorRunProgress, error) {
	published, err := wirePhaseCountsToCounts(wire.PublishedPhaseCounts)
	if err != nil {
		return CollectorRunProgress{}, err
	}
	deadLetters, err := wirePhaseCountsToCounts(wire.TerminalDeadLetterCounts)
	if err != nil {
		return CollectorRunProgress{}, err
	}
	return CollectorRunProgress{
		CollectorKind:            scope.CollectorKind(wire.CollectorKind),
		TotalWorkItems:           wire.TotalWorkItems,
		PendingWorkItems:         wire.PendingWorkItems,
		ClaimedWorkItems:         wire.ClaimedWorkItems,
		CompletedWorkItems:       wire.CompletedWorkItems,
		FailedTerminalItems:      wire.FailedTerminalItems,
		FactlessCompletedItems:   wire.FactlessCompletedItems,
		PublishedPhaseCounts:     published,
		TerminalDeadLetterCounts: deadLetters,
	}, nil
}

// claimPlaneWireRunStatus mirrors the protocol run-status response.
type claimPlaneWireRunStatus struct {
	Run          claimPlaneWireRun            `json:"run"`
	Progress     []claimPlaneWireProgress     `json:"progress,omitempty"`
	Completeness []claimPlaneWireCompleteness `json:"completeness,omitempty"`
	AsOf         time.Time                    `json:"as_of"`
}

func wireRunToRun(wire claimPlaneWireRun) (Run, error) {
	trigger := TriggerKind(wire.TriggerKind)
	if err := wireEnum("run trigger kind", trigger); err != nil {
		return Run{}, err
	}
	status := RunStatus(wire.Status)
	if err := wireEnum("run status", status); err != nil {
		return Run{}, err
	}
	return Run{
		RunID: wire.RunID, TriggerKind: trigger, Status: status,
		RequestedScopeSet: wire.RequestedScopeSet, RequestedCollector: wire.RequestedCollector,
		CreatedAt: wire.CreatedAt, UpdatedAt: wire.UpdatedAt, FinishedAt: wire.FinishedAt,
	}, nil
}

func wireRunStatusToReport(wire claimPlaneWireRunStatus) (RunStatusReport, error) {
	run, err := wireRunToRun(wire.Run)
	if err != nil {
		return RunStatusReport{}, fmt.Errorf("claim-plane GetRunStatus: %w", err)
	}
	report := RunStatusReport{Run: run, AsOf: wire.AsOf}
	for _, entry := range wire.Progress {
		progress, err := wireProgressToProgress(entry)
		if err != nil {
			return RunStatusReport{}, err
		}
		report.Progress = append(report.Progress, progress)
	}
	for _, entry := range wire.Completeness {
		// Completeness checkpoints carry no closed enums on the wire:
		// status, keyspace, and phase travel as plain strings.
		report.Completeness = append(report.Completeness, wireCompletenessToState(entry))
	}
	return report, nil
}

func wireCompletenessToState(wire claimPlaneWireCompleteness) CompletenessState {
	return CompletenessState{
		RunID: wire.RunID, CollectorKind: scope.CollectorKind(wire.CollectorKind),
		Keyspace: gpphase.Keyspace(wire.Keyspace), PhaseName: wire.PhaseName,
		Required: wire.Required, Status: wire.Status, Detail: wire.Detail,
		ObservedAt: wire.ObservedAt, UpdatedAt: wire.UpdatedAt,
	}
}

// GetRunStatus reads one run's status, per-family progress, and completeness
// checkpoints through the server.
func (c *ClaimPlaneClient) GetRunStatus(ctx context.Context, runID string) (RunStatusReport, error) {
	if strings.TrimSpace(runID) == "" {
		return RunStatusReport{}, fmt.Errorf("claim-plane GetRunStatus: run id is required")
	}
	var response claimPlaneWireRunStatus
	if err := c.post(ctx, "GetRunStatus", "/workflow.v1/GetRunStatus",
		map[string]any{"run_id": runID}, &response); err != nil {
		return RunStatusReport{}, err
	}
	return wireRunStatusToReport(response)
}

// ListRuns pages workflow runs newest first through the server. An empty
// status lists every status; a non-positive limit takes the server default.
func (c *ClaimPlaneClient) ListRuns(
	ctx context.Context,
	status RunStatus,
	limit int,
	cursor string,
) (RunPage, error) {
	if status != "" {
		if err := status.Validate(); err != nil {
			return RunPage{}, fmt.Errorf("claim-plane ListRuns: %w", err)
		}
	}
	var response struct {
		Runs       []claimPlaneWireRun `json:"runs"`
		NextCursor string              `json:"next_cursor,omitempty"`
		AsOf       time.Time           `json:"as_of"`
	}
	if err := c.post(ctx, "ListRuns", "/workflow.v1/ListRuns", map[string]any{
		"status": string(status),
		"limit":  limit,
		"cursor": cursor,
	}, &response); err != nil {
		return RunPage{}, err
	}
	page := RunPage{NextCursor: response.NextCursor, AsOf: response.AsOf}
	for _, wire := range response.Runs {
		run, err := wireRunToRun(wire)
		if err != nil {
			return RunPage{}, fmt.Errorf("claim-plane ListRuns: %w", err)
		}
		page.Runs = append(page.Runs, run)
	}
	return page, nil
}

// GetRunCompleteness reads one run's completeness checkpoints through the
// server.
func (c *ClaimPlaneClient) GetRunCompleteness(ctx context.Context, runID string) (RunCompleteness, error) {
	if strings.TrimSpace(runID) == "" {
		return RunCompleteness{}, fmt.Errorf("claim-plane GetRunCompleteness: run id is required")
	}
	var response struct {
		RunID  string                       `json:"run_id"`
		States []claimPlaneWireCompleteness `json:"states,omitempty"`
		AsOf   time.Time                    `json:"as_of"`
	}
	if err := c.post(ctx, "GetRunCompleteness", "/workflow.v1/GetRunCompleteness",
		map[string]any{"run_id": runID}, &response); err != nil {
		return RunCompleteness{}, err
	}
	got := RunCompleteness{RunID: response.RunID, AsOf: response.AsOf}
	for _, wire := range response.States {
		got.States = append(got.States, wireCompletenessToState(wire))
	}
	return got, nil
}

// QueueDepthSnapshot reads the queue-depth observation through the server.
func (c *ClaimPlaneClient) QueueDepthSnapshot(ctx context.Context) (QueueDepthSnapshot, error) {
	var response struct {
		Families []struct {
			CollectorKind string `json:"collector_kind"`
			Pending       int64  `json:"pending"`
			Claimed       int64  `json:"claimed"`
		} `json:"families,omitempty"`
		GlobalPending int64     `json:"global_pending"`
		GlobalClaimed int64     `json:"global_claimed"`
		AsOf          time.Time `json:"as_of"`
	}
	if err := c.post(ctx, "QueueDepthSnapshot", "/workflow.v1/QueueDepthSnapshot",
		nil, &response); err != nil {
		return QueueDepthSnapshot{}, err
	}
	snapshot := QueueDepthSnapshot{
		GlobalPending: response.GlobalPending,
		GlobalClaimed: response.GlobalClaimed,
		AsOf:          response.AsOf,
	}
	for _, family := range response.Families {
		snapshot.Families = append(snapshot.Families, FamilyQueueDepth{
			CollectorKind: scope.CollectorKind(family.CollectorKind),
			Pending:       family.Pending,
			Claimed:       family.Claimed,
		})
	}
	return snapshot, nil
}

// ReportRunProgress pushes one run observation to the service: the report
// plane replaces the service reading fact_records and fact_work_items, so
// core reports per-family publication counts and the service reconciles.
func (c *ClaimPlaneClient) ReportRunProgress(
	ctx context.Context,
	runID string,
	observedAt time.Time,
	progress []CollectorRunProgress,
) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("claim-plane ReportRunProgress: run id is required")
	}
	wire := make([]claimPlaneWireProgress, 0, len(progress))
	for _, entry := range progress {
		wire = append(wire, wireProgress(entry))
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "ReportRunProgress", "/workflow.v1/ReportRunProgress", map[string]any{
		"run_id":      runID,
		"observed_at": observedAt.UTC(),
		"progress":    wire,
	}, &ack)
}
