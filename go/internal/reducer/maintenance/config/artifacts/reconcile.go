// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Reconcile outcome mapping, signal emission, and cycle logging for the
// config-artifact walker. The run loop lives in runner.go; this file owns
// everything a derived repository reports.

// isRefusal reports whether outcome refuses without a statement because the
// read model is off in this process.
func isRefusal(outcome string) bool {
	return outcome == DeriveOutcomeEncodingUnsupported || outcome == DeriveOutcomeFenceMissing
}

// deriveOutcomeLabel maps an adapter result to the derives_total outcome
// label: the derive outcome, or error for a transport failure.
func deriveOutcomeLabel(outcome string, adapterErr error) string {
	if adapterErr != nil {
		return "error"
	}
	return outcome
}

// reconcileOutcome maps a derive to the reconcile_total outcome: ok repaired,
// a backfill walk that found a repository already current matched (the
// advance-only write rolled back with superseded), a repair superseded by a
// racing Write superseded, every failure error. A backfill superseded by a
// racing Write misreports match for one cycle: when the racing Write's
// derive failed the repository stays dirty and the next cycle repairs it,
// and when it succeeded the repository is already clean and the one wrong
// point simply stands; either way the next cycle labels correctly.
func reconcileOutcome(trigger, outcome string, adapterErr error) string {
	if adapterErr != nil {
		return telemetry.ConfigArtifactReconcileOutcomeError
	}
	switch outcome {
	case DeriveOutcomeOK:
		return telemetry.ConfigArtifactReconcileOutcomeRepaired
	case DeriveOutcomeSuperseded:
		if trigger == TriggerBackfill {
			return telemetry.ConfigArtifactReconcileOutcomeMatch
		}
		return telemetry.ConfigArtifactReconcileOutcomeSuperseded
	default:
		return telemetry.ConfigArtifactReconcileOutcomeError
	}
}

// emitDerive counts one walker derive on derives_total and observes its wall
// time on the derive duration histogram. A nil Instruments records nothing,
// so wiring tests run without a meter.
func (r *Runner) emitDerive(ctx context.Context, trigger, outcome string, duration time.Duration) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.ConfigArtifactDerives.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrTrigger(trigger),
		telemetry.AttrOutcome(outcome),
	))
	r.Instruments.ConfigArtifactDeriveDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		telemetry.AttrTrigger(trigger),
	))
}

// emitReconcile counts one repository on reconcile_total. A nil Instruments
// records nothing.
func (r *Runner) emitReconcile(ctx context.Context, outcome string) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.ConfigArtifactReconcile.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrOutcome(outcome),
	))
}

// recordStale sets the stale_repos gauge for every dirty-join reason,
// including zero, so a drained reason does not keep a stale-high gauge. A
// nil Instruments records nothing.
func (r *Runner) recordStale(ctx context.Context, dirty []DirtyRepo) {
	if r.Instruments == nil {
		return
	}
	counts := make(map[string]int64, len(StaleReasons))
	for _, reason := range StaleReasons {
		counts[reason] = 0
	}
	for _, item := range dirty {
		counts[item.Reason]++
	}
	for reason, count := range counts {
		r.Instruments.ConfigArtifactStaleRepos.Record(ctx, count, metric.WithAttributes(
			telemetry.AttrReason(reason),
		))
	}
}

func (r *Runner) recordFailure(ctx context.Context, err error) {
	if r.Logger != nil {
		r.Logger.ErrorContext(ctx, "config artifact walker cycle failed",
			slog.String("event_name", "config_artifacts.walker.cycle_failed"),
			log.Err(err),
			telemetry.FailureClassAttr("config_artifact_walker_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

// logCycle logs one cycle at DEBUG, or at INFO when it recorded the backfill
// marker. The counters and the stale gauge carry the per-cycle detail; the
// log carries the shape for operators without a metrics read.
func (r *Runner) logCycle(ctx context.Context, batch Batch) {
	if r.Logger == nil {
		return
	}
	attrs := []any{
		slog.String("event_name", "config_artifacts.walker.cycle"),
		slog.Int("derived", batch.Derived),
		slog.Int("repaired", batch.Repaired),
		slog.Int("matched", batch.Matched),
		slog.Int("superseded", batch.Superseded),
		slog.Int("failed", batch.Failed),
		slog.Int("skipped", batch.Skipped),
		slog.Bool("walk_wrapped", batch.WalkWrapped),
		slog.Bool("marker_recorded", batch.MarkerRecorded),
		slog.Float64("duration_seconds", batch.Duration.Seconds()),
	}
	if batch.MarkerRecorded {
		r.Logger.InfoContext(ctx, "config artifact walker recorded the backfill marker", attrs...)
		return
	}
	r.Logger.DebugContext(ctx, "config artifact walker cycle done", attrs...)
}
