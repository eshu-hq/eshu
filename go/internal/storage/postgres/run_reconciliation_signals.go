// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// recordUnknownTriggerRun emits the operator-facing signal for a #10369
// poison row: a workflow run skipped because its trigger kind is outside
// the closed workflow.TriggerKind set. The counter is unlabeled by design
// — the trigger value comes from durable storage and is unbounded, so it
// must never become a label value. The run_id and trigger text ride on the
// WARN log instead, where unbounded values are safe. A nil Instruments (the
// default) makes the counter a no-op while the log still fires, so binaries
// without a wired meter provider stay observable.
func (s *WorkflowControlStore) recordUnknownTriggerRun(ctx context.Context, run workflow.Run) {
	if s.Instruments != nil && s.Instruments.WorkflowRunUnknownTriggerSkips != nil {
		s.Instruments.WorkflowRunUnknownTriggerSkips.Add(ctx, 1)
	}
	slog.WarnContext(
		ctx, "workflow run skipped: unknown workflow trigger kind",
		slog.String("run_id", run.RunID),
		slog.String("trigger_kind", string(run.TriggerKind)),
	)
}

// recordTerminalDeadLetterBlocks emits the operator-facing signal for #4459:
// which required phase was blocked because its owning reducer domain
// dead-lettered terminally. Labeled by collector_kind and domain only
// (bounded, never run_id/scope_id/generation_id, per telemetry cardinality
// rules). A nil Instruments (the default) makes this a no-op so binaries
// without a wired meter provider are unaffected.
//
// It emits only on the reconciliation that terminates the run, which is what
// the counter documents. A dead-lettered run with pending or claimed items stays
// open and is reconciled every tick (#10091); its blocked completeness rows are
// the signal until then, and counting each tick would inflate the alert rate.
func (s *WorkflowControlStore) recordTerminalDeadLetterBlocks(
	ctx context.Context,
	run workflow.Run,
	progress []workflow.CollectorRunProgress,
	completeness []workflow.CompletenessState,
) {
	if run.Status != workflow.RunStatusFailed {
		return
	}
	deadLetterCountsByCollector := make(map[string]map[workflow.PhasePublicationKey]int, len(progress))
	for _, collector := range progress {
		deadLetterCountsByCollector[string(collector.CollectorKind)] = collector.TerminalDeadLetterCounts
	}

	for _, state := range completeness {
		if state.Status != workflow.CompletenessStatusBlocked {
			continue
		}
		counts := deadLetterCountsByCollector[string(state.CollectorKind)]
		key := workflow.PhasePublicationKey{
			Keyspace:  state.Keyspace,
			PhaseName: gpphase.Phase(state.PhaseName),
		}
		if counts[key] <= 0 {
			// Blocked for a different reason (terminal collector failure),
			// not a bridged reducer dead-letter. Do not attribute it here.
			continue
		}
		domain := workflowReducerDeadLetterDomainForPhase(state.PhaseName)
		if s.Instruments != nil && s.Instruments.WorkflowRunTerminalDeadLetterBlocks != nil {
			s.Instruments.WorkflowRunTerminalDeadLetterBlocks.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrCollectorKind(string(state.CollectorKind)),
				telemetry.AttrDomain(domain),
			))
		}
		slog.WarnContext(
			ctx, "workflow run blocked by terminal reducer dead-letter",
			slog.String("run_id", run.RunID),
			slog.String("collector_kind", string(state.CollectorKind)),
			slog.String("keyspace", string(state.Keyspace)),
			slog.String("phase", state.PhaseName),
			slog.String("domain", domain),
			slog.Int("dead_lettered_work_items", counts[key]),
		)
	}
}

// workflowReducerDeadLetterDomainForPhase reports the reducer domain
// attributed to a blocked phase name for the log/metric emitted by
// recordTerminalDeadLetterBlocks. Kept in lockstep with
// workflowReducerDeadLetterPhaseBridgeSQL and the DeadLetterDomain values in
// collector_contract.go: today both bridged phases carry the same string as
// their owning domain, but this indirection keeps the log/metric correct even
// if that coincidence ever changes.
func workflowReducerDeadLetterDomainForPhase(phaseName string) string {
	switch phaseName {
	case string(gpphase.PhaseDeploymentMapping):
		return string(reducer.DomainDeploymentMapping)
	case string(gpphase.PhaseWorkloadMaterialization):
		return string(reducer.DomainWorkloadMaterialization)
	default:
		return phaseName
	}
}
