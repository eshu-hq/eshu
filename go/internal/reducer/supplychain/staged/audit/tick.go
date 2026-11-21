// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"context"
	"errors"
	"log/slog"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// SweepStore is the writer surface of the sweep: the persisted cursor row and
// the per-range verify. postgres.SupplyChainImpactWinnersStore implements it.
type SweepStore interface {
	ReadWinnersAuditSweep(ctx context.Context) (postgres.WinnersAuditSweep, error)
	VerifyWinnersAuditRange(ctx context.Context, v postgres.WinnersAuditRangeVerify) (postgres.WinnersAuditRangeOutcome, error)
}

// Range outcomes the maintainer records on the range span and histogram.
const (
	OutcomeAdvanced   = "advanced"
	OutcomeCompleted  = "completed"
	OutcomeSkipped    = "skipped"
	OutcomeLockBusy   = "lock_busy"
	OutcomeCursorRace = "cursor_race"
	OutcomeDeferred   = "deferred"
)

// StagedAuditor runs the staged winners audit as a sweep of identifier ranges
// over the keyed winners index (#11131). Each AuditWinners call runs one
// range: one standby statement recomputes the range's winners and diffs them
// against the stored winners in the same key interval, then one writer
// transaction verifies the differing identifiers, queues the stale ones, and
// advances the persisted cursor by compare-and-set. The maintainer calls it
// back to back while the result reports More, so one audit tick covers many
// ranges and a roll resumes from the stored cursor.
//
// Concurrency contract: the cursor's compare-and-set makes two instances
// advance each range at most once; the loser rolls its repairs back. The
// resweep lock is taken only when a candidate nothing has queued remains, by
// try-lock first and by a lock_timeout-bounded waiting claim after two misses
// on the same range, so a keyed tick at full duty cannot starve the sweep and
// loses at most one verify per locked range.
type StagedAuditor struct {
	// Fleet pins the range's reader member. Required.
	Fleet pgaccess.StagedAuditFleet
	// Store reads the cursor and runs the writer-side range verify. Required.
	Store SweepStore
	// Telemetry is the shared recorder. Nil records nowhere.
	Telemetry *pgaccess.StagedAuditTelemetry
	// BatchTimeout bounds one standby statement. Zero selects
	// pgaccess.DefaultStagedAuditBatchTimeout (25 s).
	BatchTimeout time.Duration
	// RangeBudget is the fact-row budget of one range. Zero selects
	// postgres.DefaultWinnersAuditRangeBudget. A timed-out range retries at
	// half the budget, down to one row (one whole identifier).
	RangeBudget int
	// Logger receives one line per range and per completed sweep. Nil logs
	// nothing.
	Logger *slog.Logger

	// ReadRange and ProbeNullKeys default to the postgres statements; tests
	// replace them.
	ReadRange     func(ctx context.Context, reader db.Queryer, lo *postgres.WinnersAuditRangeIdent, budget int) (postgres.WinnersAuditRange, error)
	ProbeNullKeys func(ctx context.Context, reader db.Queryer) (postgres.WinnersAuditNullKeys, error)
}

var _ supplychaincore.SupplyChainImpactWinnersAuditor = (*StagedAuditor)(nil)

// AuditWinners runs one range of the sweep. A deferred range never reached
// the writer, except the last range when the end NULL probe found a NULL,
// which commits its repairs and restarts the sweep without counting it.
func (a *StagedAuditor) AuditWinners(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersAudit, error) {
	// RED stub (#11131): the whole-corpus tick still defers on its budget.
	return a.deferred(ctx, pgaccess.StagedAuditDeferBudget, time.Now(), errors.New("staged audit over budget")), nil
}

// deferred records a deferral and returns the deferred result.
func (a *StagedAuditor) deferred(ctx context.Context, reason pgaccess.StagedAuditDeferralReason, started time.Time, cause error) supplychaincore.SupplyChainImpactWinnersAudit {
	a.Telemetry.ObserveDeferral(reason)
	if a.Logger != nil {
		attrs := []any{slog.String("reason", string(reason)), slog.Float64("read_seconds", time.Since(started).Seconds())}
		if cause != nil {
			attrs = append(attrs, slog.String("error", cause.Error()))
		}
		a.Logger.InfoContext(ctx, "staged winners audit deferred", attrs...)
	}
	return supplychaincore.SupplyChainImpactWinnersAudit{
		Deferred: true, Partial: true, Outcome: OutcomeDeferred, ReadDuration: time.Since(started),
	}
}

func cursorText(c *postgres.WinnersAuditRangeIdent) string {
	if c == nil {
		return ""
	}
	return c.PackageID + "|" + c.RepositoryID + "|" + c.SubjectDigest + "|" + c.C0 + "|" + c.AdvisoryID
}
