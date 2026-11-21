// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

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

// lockMissesBeforeWait is how many try-lock misses on one range the verify
// takes before it queues on the waiting claim (#11131 decision 3).
const lockMissesBeforeWait = 2

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

	mu       sync.Mutex
	missFor  string
	missRuns int
}

var _ supplychaincore.SupplyChainImpactWinnersAuditor = (*StagedAuditor)(nil)

// AuditWinners runs one range of the sweep. A deferred range never reached
// the writer, except the last range when the end NULL probe found a NULL,
// which commits its repairs and restarts the sweep without counting it.
func (a *StagedAuditor) AuditWinners(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersAudit, error) {
	var none supplychaincore.SupplyChainImpactWinnersAudit
	started := time.Now()
	sweep, err := a.Store.ReadWinnersAuditSweep(ctx)
	if err != nil {
		return none, fmt.Errorf("read winners audit sweep: %w", err)
	}
	a.Telemetry.ObserveSweepState(stagedSweepState(sweep))
	member, err := a.Fleet.PinMember(ctx)
	if err != nil {
		return a.deferred(ctx, pgaccess.StagedAuditDeferBatchAbort, started, fmt.Errorf("pin staged audit member: %w", err)), nil
	}
	if sweep.Cursor == nil {
		if reason, err := a.probe(ctx, member); err != nil || reason != "" {
			return a.deferred(ctx, reasonOr(reason, err), started, err), nil
		}
	}
	rng, reason, err := a.readRange(ctx, member, sweep.Cursor)
	if err != nil {
		return a.deferred(ctx, reason, started, err), nil
	}
	read := time.Since(started)
	if rng.Pipe {
		return a.deferred(ctx, pgaccess.StagedAuditDeferPipe, started, errors.New("a range fact has '|' in an identifier field")), nil
	}
	complete := false
	if rng.Last {
		reason, err := a.probe(ctx, member)
		if err != nil {
			return a.deferred(ctx, reasonOr(reason, err), started, err), nil
		}
		// A NULL at the end voids the sweep: the last range still verifies
		// and restarts the sweep, but nothing records it as complete.
		complete = reason == ""
		if !complete {
			a.Telemetry.ObserveDeferral(pgaccess.StagedAuditDeferNullKey)
		}
	}
	outcome, err := a.Store.VerifyWinnersAuditRange(ctx, postgres.WinnersAuditRangeVerify{
		SweepID: sweep.SweepID, Range: rng, WaitForLock: a.waitForLock(sweep, rng), Complete: complete,
	})
	if err != nil {
		return none, fmt.Errorf("verify winners audit range: %w", err)
	}
	return a.report(ctx, sweep, rng, outcome, read, time.Since(started)), nil
}

// report maps the range transaction to the maintainer's result and records it.
func (a *StagedAuditor) report(ctx context.Context, sweep postgres.WinnersAuditSweep, rng postgres.WinnersAuditRange,
	outcome postgres.WinnersAuditRangeOutcome, read, elapsed time.Duration,
) supplychaincore.SupplyChainImpactWinnersAudit {
	result := supplychaincore.SupplyChainImpactWinnersAudit{ReadDuration: read, Partial: true, More: true}
	switch {
	case outcome.Skipped:
		a.noteMiss(sweep, rng)
		result.Skipped, result.Outcome = true, OutcomeSkipped
	case outcome.LockBusy:
		a.Telemetry.ObserveDeferral(pgaccess.StagedAuditDeferLockBusy)
		result.Outcome = OutcomeLockBusy
	case outcome.CursorRace:
		a.clearMiss()
		a.Telemetry.ObserveDeferral(pgaccess.StagedAuditDeferCursorRace)
		result.Outcome = OutcomeCursorRace
	default:
		a.clearMiss()
		result.Repairs, result.Outcome = outcome.Repairs, OutcomeAdvanced
		result.More = !rng.Last
		if outcome.Completed {
			result.Partial, result.Outcome = false, OutcomeCompleted
		}
		a.Telemetry.ObserveRange(pgaccess.StagedAuditRangeObservation{
			Facts: rng.Facts, Candidates: outcome.Candidates, Pending: outcome.Pending, Repairs: outcome.Repairs,
			Discards: outcome.Discards, Duration: elapsed, Oversize: rng.Oversize, Budget: rng.Budget,
		})
		a.Telemetry.ObserveVerify(outcome.Discards)
		a.Telemetry.ObserveSweepState(stagedSweepState(outcome.Sweep))
	}
	if a.Logger != nil {
		a.Logger.InfoContext(ctx, "staged winners audit range",
			slog.String("outcome", result.Outcome), slog.Int64("sweep_id", sweep.SweepID),
			slog.Int64("ranges_done", outcome.Sweep.RangesDone), slog.Bool("last", rng.Last),
			slog.Bool("oversize", rng.Oversize), slog.Int("budget", rng.Budget), slog.Int64("facts", rng.Facts),
			slog.Int64("candidates", outcome.Candidates), slog.Int64("pending", outcome.Pending),
			slog.Int64("repairs", outcome.Repairs), slog.Int64("discards", outcome.Discards),
			slog.Bool("locked", outcome.Locked), slog.Float64("read_seconds", read.Seconds()),
			slog.Float64("range_seconds", elapsed.Seconds()), slog.String("cursor", cursorText(rng.Hi)))
		if outcome.Completed {
			a.Logger.InfoContext(ctx, "staged winners audit sweep completed",
				slog.Int64("sweep_id", sweep.SweepID), slog.Float64("sweep_seconds", outcome.Sweep.LastSeconds))
		}
	}
	return result
}

// readRange reads one range, halving the budget on each statement timeout
// down to one row. It returns the deferral reason with any error.
func (a *StagedAuditor) readRange(ctx context.Context, member string, lo *postgres.WinnersAuditRangeIdent) (postgres.WinnersAuditRange, pgaccess.StagedAuditDeferralReason, error) {
	read := a.ReadRange
	if read == nil {
		read = postgres.ReadWinnersAuditRange
	}
	budget := a.RangeBudget
	if budget <= 0 {
		budget = postgres.DefaultWinnersAuditRangeBudget
	}
	for {
		var rng postgres.WinnersAuditRange
		err := a.onMember(ctx, member, func(stmtCtx context.Context, reader db.Queryer) error {
			var err error
			rng, err = read(stmtCtx, reader, lo, budget)
			return err
		})
		if err == nil {
			return rng, "", nil
		}
		if ctx.Err() != nil || !isStatementTimeout(err) {
			return rng, classifyRead(err), err
		}
		if budget == 1 {
			return rng, pgaccess.StagedAuditDeferTimeout, fmt.Errorf("one-identifier range timed out: %w", err)
		}
		budget = max(1, budget/2)
		a.Telemetry.ObserveRangeRetry()
	}
}

// probe runs the NULL probe and returns null_key when a field is NULL.
func (a *StagedAuditor) probe(ctx context.Context, member string) (pgaccess.StagedAuditDeferralReason, error) {
	probe := a.ProbeNullKeys
	if probe == nil {
		probe = postgres.ProbeWinnersAuditNullKeys
	}
	var keys postgres.WinnersAuditNullKeys
	err := a.onMember(ctx, member, func(stmtCtx context.Context, reader db.Queryer) error {
		var err error
		keys, err = probe(stmtCtx, reader)
		return err
	})
	if err != nil {
		if isStatementTimeout(err) && ctx.Err() == nil {
			return pgaccess.StagedAuditDeferTimeout, err
		}
		return classifyRead(err), err
	}
	if keys.Any() {
		return pgaccess.StagedAuditDeferNullKey, nil
	}
	return "", nil
}

// onMember runs fn on the pinned member under the per-statement guard.
func (a *StagedAuditor) onMember(ctx context.Context, member string, fn func(ctx context.Context, reader db.Queryer) error) error {
	limit := a.BatchTimeout
	if limit <= 0 {
		limit = pgaccess.DefaultStagedAuditBatchTimeout
	}
	stmtCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return a.Fleet.RunOnMember(stmtCtx, member, fn)
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

// waitForLock reports whether this range has missed the try-lock twice.
func (a *StagedAuditor) waitForLock(sweep postgres.WinnersAuditSweep, rng postgres.WinnersAuditRange) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.missFor == rangeKey(sweep, rng) && a.missRuns >= lockMissesBeforeWait
}

func (a *StagedAuditor) noteMiss(sweep postgres.WinnersAuditSweep, rng postgres.WinnersAuditRange) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := rangeKey(sweep, rng)
	if a.missFor != key {
		a.missFor, a.missRuns = key, 0
	}
	a.missRuns++
}

func (a *StagedAuditor) clearMiss() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.missFor, a.missRuns = "", 0
}

// rangeKey names a range by its sweep and the cursor it starts after.
func rangeKey(sweep postgres.WinnersAuditSweep, rng postgres.WinnersAuditRange) string {
	return fmt.Sprintf("%d\x1e%s", sweep.SweepID, cursorText(rng.Lo))
}

func cursorText(c *postgres.WinnersAuditRangeIdent) string {
	if c == nil {
		return ""
	}
	return c.PackageID + "|" + c.RepositoryID + "|" + c.SubjectDigest + "|" + c.C0 + "|" + c.AdvisoryID
}

func stagedSweepState(s postgres.WinnersAuditSweep) pgaccess.StagedAuditSweepState {
	return pgaccess.StagedAuditSweepState{
		SweepID: s.SweepID, RangesDone: s.RangesDone, LastCompletedAt: s.LastCompletedAt,
		LastSeconds: s.LastSeconds, LastRanges: s.LastRanges, LastFacts: s.LastFacts,
		LastCandidates: s.LastCandidates, LastRepairs: s.LastRepairs, LastDiscards: s.LastDiscards,
	}
}

func reasonOr(reason pgaccess.StagedAuditDeferralReason, err error) pgaccess.StagedAuditDeferralReason {
	if reason != "" {
		return reason
	}
	return classifyRead(err)
}

// classifyRead maps a standby statement failure to its deferral reason.
func classifyRead(err error) pgaccess.StagedAuditDeferralReason {
	var deferred *pgaccess.StagedAuditDeferredError
	if errors.As(pgaccess.ClassifyStagedAuditAbort(err), &deferred) {
		return deferred.Reason
	}
	return pgaccess.StagedAuditDeferBatchAbort
}

// isStatementTimeout reports a per-statement guard expiry: the context
// deadline, or SQLSTATE 57014 (query_canceled) that pgx raises for it.
func isStatementTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "57014"
}
