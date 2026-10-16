// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package consumption

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Phase names the backfill phase that asked for a scope rebuild. It is the
// closed value set of the rebuild counter's phase label.
type Phase string

const (
	// PhaseInitial is the first walk over every scope with sidecar facts.
	PhaseInitial Phase = "initial"
	// PhaseDirtyRepair rebuilds a scope an old writer marked dirty.
	PhaseDirtyRepair Phase = "dirty_repair"
)

// Outcome is the closed result of one scope rebuild.
type Outcome string

const (
	// OutcomeCleared means every page committed and the dirty row was still
	// the version the rebuild wrote, so the rebuild removed it.
	OutcomeCleared Outcome = "cleared"
	// OutcomeLeftDirty means every page committed but another writer
	// rewrote the dirty row during the rebuild; a later rebuild repairs it.
	OutcomeLeftDirty Outcome = "left_dirty"
	// OutcomeSkipped means a dirty repair found no dirty row left to repair.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeDisappeared means the scope row was gone; any dirty row was
	// removed because the scope's facts and keys cascaded away with it.
	OutcomeDisappeared Outcome = "disappeared"
	// OutcomeLockTimeout means a rebuild transaction hit lock_timeout
	// (SQLSTATE 55P03). The wait may be on the scope row (an ingester holding
	// it), the dirty-scope row, or a sidecar key row; the scope_failed log
	// line's error text names which.
	OutcomeLockTimeout Outcome = "lock_timeout"
	// OutcomeFailed means a rebuild transaction failed for another reason,
	// including cancellation.
	OutcomeFailed Outcome = "failed"
)

const (
	transactionOutcomeCommitted   = "committed"
	transactionOutcomeLockTimeout = "lock_timeout"
	transactionOutcomeFailed      = "failed"
)

// Result describes one scope rebuild for the progress log. Pages and Facts
// count only committed fact pages; Transactions counts every attempt,
// including the start transaction and a failed one.
type Result struct {
	ScopeID        string
	Phase          Phase
	Outcome        Outcome
	Pages          int
	Facts          int
	Transactions   int
	MaxTransaction time.Duration
	LockWait       time.Duration
	MaxLockWait    time.Duration
	Duration       time.Duration
}

func outcomeForError(err error) Outcome {
	if coordination.IsLockNotAvailable(err) {
		return OutcomeLockTimeout
	}
	return OutcomeFailed
}

func (r Rebuilder) recordTransaction(ctx context.Context, result *Result, duration time.Duration, err error) {
	result.Transactions++
	result.MaxTransaction = max(result.MaxTransaction, duration)
	if r.Instruments == nil || r.Instruments.PackageManifestBackfillPageDuration == nil {
		return
	}
	outcome := transactionOutcomeCommitted
	if err != nil {
		outcome = transactionOutcomeFailed
		if coordination.IsLockNotAvailable(err) {
			outcome = transactionOutcomeLockTimeout
		}
	}
	r.Instruments.PackageManifestBackfillPageDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome)))
}

func (r Rebuilder) recordLockWait(ctx context.Context, result *Result, wait time.Duration) {
	result.LockWait += wait
	result.MaxLockWait = max(result.MaxLockWait, wait)
	if r.Instruments == nil || r.Instruments.PackageManifestBackfillScopeLockWait == nil {
		return
	}
	r.Instruments.PackageManifestBackfillScopeLockWait.Record(ctx, wait.Seconds())
}

func (r Rebuilder) recordRebuild(ctx context.Context, result Result) {
	if r.Instruments == nil || r.Instruments.PackageManifestBackfillScopeRebuilds == nil {
		return
	}
	r.Instruments.PackageManifestBackfillScopeRebuilds.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionPhase, string(result.Phase)),
		attribute.String(telemetry.MetricDimensionOutcome, string(result.Outcome)),
	))
}
