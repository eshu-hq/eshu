// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeintel

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// defaultCodeReachabilityMaxPollInterval caps the #10870 exponential
	// idle backoff. It is the intent-completion-driven pickup bound: work
	// whose completion advances the cursor is picked up at most this long
	// after the completing intent commits, plus one full-query drain.
	defaultCodeReachabilityMaxPollInterval = 30 * time.Second
	// defaultCodeReachabilityAuditInterval forces the full candidate query
	// even when the cursor probe has not moved (#10870). It bounds every
	// other readiness-transition delay (a materialization ack landing after
	// all intent completions, a generation activation, a reopen).
	defaultCodeReachabilityAuditInterval = 15 * time.Minute
)

// ProcessNextCycle runs one probe-gated cycle (#10870): a cheap cursor probe
// first, the expensive full candidate query only when the probe advanced
// past the cursor, the probe failed, the probe regressed, the cursor is
// unknown, or the audit interval elapsed.
//
// Cost contract: a probe-equal cycle with no due audit costs one index-only
// probe and no full query. Every full query refreshes the audit clock and,
// when the probe answered, moves the cursor to the probe value read BEFORE
// the query (never a post-query max), so completions landing during the
// query are caught next cycle.
func (r *CodeReachabilityProjectionRunner) ProcessNextCycle(
	ctx context.Context,
	now time.Time,
) (CodeReachabilityProjectionResult, error) {
	if err := r.validate(); err != nil {
		return CodeReachabilityProjectionResult{}, err
	}
	if r.Config.DisableIdleProbe {
		return r.runFullCycle(ctx, now, time.Time{}, false, telemetry.CodeReachabilityCycleKindAuditFull)
	}
	probeAt, probeOK, err := r.InputLoader.LatestCompletedIntentTime(ctx)
	if err != nil || !probeOK {
		return r.runFullCycle(ctx, now, probeAt, probeOK, telemetry.CodeReachabilityCycleKindAuditFull)
	}
	if r.cursorKnown {
		switch {
		case probeAt.After(r.cursor):
			return r.runFullCycle(ctx, now, probeAt, true, telemetry.CodeReachabilityCycleKindProbeTriggeredFull)
		case probeAt.Equal(r.cursor):
			if !r.auditDue(now) {
				r.recordCycle(ctx, telemetry.CodeReachabilityCycleKindProbeSkip, "", 0)
				return CodeReachabilityProjectionResult{}, nil
			}
		}
	}
	return r.runFullCycle(ctx, now, probeAt, true, telemetry.CodeReachabilityCycleKindAuditFull)
}

// runFullCycle runs the full candidate query, refreshes the audit clock,
// advances the cursor to the probe value read before the query when the
// probe answered, and records the cycle kind (productive when the query
// found work, kind otherwise) with the trigger that forced the query.
func (r *CodeReachabilityProjectionRunner) runFullCycle(
	ctx context.Context,
	now time.Time,
	probeAt time.Time,
	probeOK bool,
	kind string,
) (CodeReachabilityProjectionResult, error) {
	result, err := r.ProcessOnce(ctx, now)
	if err != nil {
		return CodeReachabilityProjectionResult{}, err
	}
	r.lastFullAudit = now
	if probeOK {
		r.cursor = probeAt
		r.cursorKnown = true
	}
	if result.InputsProcessed > 0 {
		trigger := telemetry.CodeReachabilityTriggerProbe
		if kind == telemetry.CodeReachabilityCycleKindAuditFull {
			trigger = telemetry.CodeReachabilityTriggerAudit
		}
		r.recordCycle(ctx, telemetry.CodeReachabilityCycleKindProductive, trigger, result.InputsProcessed)
		if kind == telemetry.CodeReachabilityCycleKindAuditFull && r.Logger != nil {
			r.Logger.InfoContext(
				ctx,
				"code reachability projection audit found work the probe did not surface",
				slog.Int("input_count", result.InputsProcessed),
				slog.Int("row_count", result.RowsWritten),
			)
		}
		return result, nil
	}
	if kind == telemetry.CodeReachabilityCycleKindAuditFull {
		r.logAuditCycle(ctx, result)
	}
	r.recordCycle(ctx, kind, "", 0)
	return result, nil
}

// logAuditCycle logs every audit-forced full query with its found count,
// even when empty: the audit is the short-list-miss backstop, and its
// cadence plus found count is the operator's proof the backstop runs.
func (r *CodeReachabilityProjectionRunner) logAuditCycle(ctx context.Context, result CodeReachabilityProjectionResult) {
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"code reachability projection audit completed",
		slog.Int("input_count", result.InputsProcessed),
		slog.Float64("duration_seconds", result.DurationSeconds),
	)
}

// auditDue reports whether the full-query audit is due at now. A runner
// that never ran a full query (zero lastFullAudit) is always due.
func (r *CodeReachabilityProjectionRunner) auditDue(now time.Time) bool {
	return now.Sub(r.lastFullAudit) >= r.auditInterval()
}

func (r *CodeReachabilityProjectionRunner) maxPollInterval() time.Duration {
	if r.Config.MaxPollInterval <= 0 {
		return defaultCodeReachabilityMaxPollInterval
	}
	return r.Config.MaxPollInterval
}

func (r *CodeReachabilityProjectionRunner) auditInterval() time.Duration {
	if r.Config.AuditInterval <= 0 {
		return defaultCodeReachabilityAuditInterval
	}
	return r.Config.AuditInterval
}

// recordCycle records one idle-gated cycle kind, and — on productive cycles
// only — how many inputs the full query found and which trigger surfaced
// them. Nil instruments record nothing.
func (r *CodeReachabilityProjectionRunner) recordCycle(
	ctx context.Context,
	kind string,
	trigger string,
	inputs int,
) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.CodeReachabilityProjectionCycles.Add(
		ctx,
		1,
		metric.WithAttributes(telemetry.AttrKind(kind)),
	)
	if kind != telemetry.CodeReachabilityCycleKindProductive {
		return
	}
	r.Instruments.CodeReachabilityProjectionInputsFound.Add(
		ctx,
		int64(inputs),
		metric.WithAttributes(telemetry.AttrTrigger(trigger)),
	)
}

// recordBackoff samples the current idle wait: the computed backoff after
// an empty cycle, 0 on a productive cycle that re-polls immediately.
// Nil instruments record nothing.
func (r *CodeReachabilityProjectionRunner) recordBackoff(ctx context.Context, backoff time.Duration) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.CodeReachabilityProjectionBackoff.Record(ctx, backoff.Seconds())
}
