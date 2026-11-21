// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// WinnersAuditSweep is the persisted sweep row (migration 335).
type WinnersAuditSweep struct {
	// SweepID numbers the sweep in progress; it grows by one at each sweep end.
	SweepID int64
	// Cursor is the last identifier of the last completed range; nil at the
	// start of a sweep.
	Cursor *WinnersAuditRangeIdent
	// RangesDone counts the ranges this sweep has committed.
	RangesDone int64
	// StartedAt is when the sweep's first range committed; zero at the start.
	StartedAt time.Time
	// LastCompletedAt is when the last completed sweep ended; zero if none.
	LastCompletedAt time.Time
	// LastSeconds, LastRanges, LastFacts, LastCandidates, LastRepairs and
	// LastDiscards describe the last completed sweep.
	LastSeconds    float64
	LastRanges     int64
	LastFacts      int64
	LastCandidates int64
	LastRepairs    int64
	LastDiscards   int64
}

// ReadWinnersAuditSweep reads the sweep row on the writer, creating it on
// first use.
func (s SupplyChainImpactWinnersStore) ReadWinnersAuditSweep(ctx context.Context) (WinnersAuditSweep, error) {
	var none WinnersAuditSweep
	if s.DB == nil {
		return none, fmt.Errorf("supply chain impact winners database is required")
	}
	rows, err := s.DB.QueryContext(ctx, selectWinnersAuditSweepSQL)
	if err != nil {
		return none, fmt.Errorf("read winners audit sweep: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return none, fmt.Errorf("read winners audit sweep: %w", err)
		}
		return none, fmt.Errorf("read winners audit sweep: statement returned no row")
	}
	var (
		out                             WinnersAuditSweep
		pkg, repo, digest, c0, advisory sql.NullString
		started, completed              sql.NullTime
		seconds                         sql.NullFloat64
		ranges, facts, cands, reps, dis sql.NullInt64
	)
	if err := rows.Scan(&out.SweepID, &pkg, &repo, &digest, &c0, &advisory, &out.RangesDone, &started, &completed,
		&seconds, &ranges, &facts, &cands, &reps, &dis); err != nil {
		return none, fmt.Errorf("scan winners audit sweep: %w", err)
	}
	if pkg.Valid {
		out.Cursor = &WinnersAuditRangeIdent{pkg.String, repo.String, digest.String, c0.String, advisory.String}
	}
	out.StartedAt, out.LastCompletedAt = started.Time, completed.Time
	out.LastSeconds = seconds.Float64
	out.LastRanges, out.LastFacts, out.LastCandidates = ranges.Int64, facts.Int64, cands.Int64
	out.LastRepairs, out.LastDiscards = reps.Int64, dis.Int64
	return out, rows.Err()
}

// WinnersAuditRangeVerify is one range handed to the writer.
type WinnersAuditRangeVerify struct {
	// SweepID is the sweep the range was read in.
	SweepID int64
	// Range is the standby read.
	Range WinnersAuditRange
	// WaitForLock takes the resweep lock with the waiting claim bounded by
	// WinnersAuditRangeLockTimeout instead of the try-lock.
	WaitForLock bool
	// Complete records the sweep as completed. It is valid only on the last
	// range, and only when the NULL probe read clean at both ends.
	Complete bool
}

// WinnersAuditRangeOutcome is what the range transaction did.
type WinnersAuditRangeOutcome struct {
	// Advanced reports that the cursor moved and the transaction committed.
	Advanced bool
	// Skipped reports a lost try-lock; nothing committed.
	Skipped bool
	// LockBusy reports that the waiting claim hit its lock_timeout; nothing
	// committed.
	LockBusy bool
	// CursorRace reports that another instance advanced this range first;
	// the transaction, repairs included, rolled back.
	CursorRace bool
	// Locked reports that the verify took the resweep lock.
	Locked bool
	// Candidates, Pending and Repairs count the range's differing
	// identifiers, those already queued, and the stale ones this range
	// queued. Discards are candidates that were not repaired.
	Candidates, Pending, Repairs, Discards int64
	// Completed reports that this range completed the sweep.
	Completed bool
	// Sweep is the cursor row after the commit: its SweepID, RangesDone and
	// last completion.
	Sweep WinnersAuditSweep
}

// VerifyWinnersAuditRange runs one range's writer transaction: seed the
// candidates, drop those already queued (no lock), and only when a
// candidate remains take the resweep lock (try-lock, or the waiting claim),
// verify it with the keyed statements, queue the stale identifiers nothing
// has queued since, and compare-and-set the cursor. A lost claim or a lost
// compare-and-set rolls everything back.
func (s SupplyChainImpactWinnersStore) VerifyWinnersAuditRange(ctx context.Context, v WinnersAuditRangeVerify) (WinnersAuditRangeOutcome, error) {
	var none WinnersAuditRangeOutcome
	if v.Complete && !v.Range.Last {
		return none, fmt.Errorf("verify winners audit range: only the last range can complete a sweep")
	}
	beginner, ok := s.DB.(db.Beginner)
	if !ok {
		return none, fmt.Errorf("supply chain impact winners database must support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin winners audit range: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	out := WinnersAuditRangeOutcome{Candidates: int64(len(v.Range.Candidates))}
	if len(v.Range.Candidates) > 0 {
		locked, skip, err := s.verifyWinnersAuditRangeCandidates(ctx, tx, v, &out)
		if err != nil {
			if isLockTimeoutError(err) {
				return WinnersAuditRangeOutcome{LockBusy: true, Candidates: out.Candidates}, nil
			}
			return none, err
		}
		if skip {
			return WinnersAuditRangeOutcome{Skipped: true, Candidates: out.Candidates}, nil
		}
		out.Locked = locked
	}
	out.Discards = out.Candidates - out.Repairs
	advanced, err := advanceWinnersAuditSweep(ctx, tx, v, out)
	if err != nil {
		if isLockTimeoutError(err) {
			return WinnersAuditRangeOutcome{LockBusy: true, Candidates: out.Candidates}, nil
		}
		return none, err
	}
	if advanced == nil {
		return WinnersAuditRangeOutcome{CursorRace: true, Candidates: out.Candidates, Locked: out.Locked}, nil
	}
	if err := tx.Commit(); err != nil {
		return none, fmt.Errorf("commit winners audit range: %w", err)
	}
	out.Advanced, out.Completed, out.Sweep = true, v.Complete, *advanced
	return out, nil
}

// verifyWinnersAuditRangeCandidates is the candidate half of the range
// transaction. It reports whether it took the lock and whether the try-lock
// lost (skip).
func (s SupplyChainImpactWinnersStore) verifyWinnersAuditRangeCandidates(
	ctx context.Context, tx db.Transaction, v WinnersAuditRangeVerify, out *WinnersAuditRangeOutcome,
) (locked, skip bool, err error) {
	if _, err := tx.ExecContext(ctx, createWinnersAuditStaleIdentsSQL); err != nil {
		return false, false, fmt.Errorf("seed winners audit range candidates: %w", err)
	}
	if err := seedWinnersAuditStaleIdents(ctx, tx, v.Range.Candidates); err != nil {
		return false, false, err
	}
	for _, stmt := range []string{analyzeWinnersKeyedIdentsSQL, createWinnersAuditRangePendingSQL, dropWinnersAuditRangePendingIdentsSQL} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return false, false, fmt.Errorf("filter winners audit range candidates: %w", err)
		}
	}
	remaining, err := scanWinnersInt64(ctx, tx, countWinnersAuditRangeRemainingSQL, "count winners audit range candidates")
	if err != nil {
		return false, false, err
	}
	out.Pending = out.Candidates - remaining
	if remaining == 0 {
		return false, false, nil
	}
	if v.WaitForLock {
		if _, err := tx.ExecContext(ctx, setWinnersAuditRangeLockTimeoutSQL); err != nil {
			return false, false, fmt.Errorf("bound winners audit range lock wait: %w", err)
		}
		if _, err := tx.ExecContext(ctx, waitClaimSupplyChainImpactWinnersResweepSQL); err != nil {
			return false, false, fmt.Errorf("claim supply chain impact winners resweep (waiting): %w", err)
		}
	} else {
		claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx)
		if err != nil {
			return false, false, err
		}
		if !claimed {
			return false, true, nil
		}
	}
	for _, stmt := range []string{
		analyzeWinnersKeyedIdentsSQL,
		buildWinnersKeyedActiveFactsSQL, addWinnersKeyedOperatorFactsSQL, analyzeWinnersKeyedActiveFactsSQL,
		createWinnersAuditStaleSQL, createWinnersAuditRangeStalePendingSQL,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return true, false, fmt.Errorf("verify winners audit range candidates: %w", err)
		}
	}
	queued, err := tx.ExecContext(ctx, queueWinnersAuditRepairsSQL)
	if err != nil {
		return true, false, fmt.Errorf("queue winners audit range repairs: %w", err)
	}
	if out.Repairs, err = queued.RowsAffected(); err != nil {
		return true, false, fmt.Errorf("read winners audit range repairs: %w", err)
	}
	return true, false, nil
}

// advanceWinnersAuditSweep runs the compare-and-set. It returns nil, nil when
// the cursor had moved (zero rows).
func advanceWinnersAuditSweep(ctx context.Context, tx db.Transaction, v WinnersAuditRangeVerify, out WinnersAuditRangeOutcome) (*WinnersAuditSweep, error) {
	args := []any{v.SweepID}
	args = append(args, winnersAuditCursorArgs(v.Range.Lo)...)
	var hi *WinnersAuditRangeIdent
	if !v.Range.Last {
		hi = v.Range.Hi
	}
	args = append(args, winnersAuditCursorArgs(hi)...)
	args = append(args, v.Range.Last, v.Complete, v.Range.Facts, out.Candidates, out.Repairs, out.Discards)
	rows, err := tx.QueryContext(ctx, advanceWinnersAuditSweepSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("advance winners audit sweep: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("advance winners audit sweep: %w", err)
		}
		return nil, nil
	}
	var (
		sweep     WinnersAuditSweep
		completed sql.NullTime
		seconds   sql.NullFloat64
	)
	if err := rows.Scan(&sweep.SweepID, &sweep.RangesDone, &completed, &seconds); err != nil {
		return nil, fmt.Errorf("scan winners audit sweep advance: %w", err)
	}
	sweep.Cursor, sweep.LastCompletedAt, sweep.LastSeconds = hi, completed.Time, seconds.Float64
	return &sweep, rows.Err()
}

// winnersAuditCursorArgs renders a cursor as five text arguments, all NULL
// for a nil cursor.
func winnersAuditCursorArgs(c *WinnersAuditRangeIdent) []any {
	if c == nil {
		return []any{nil, nil, nil, nil, nil}
	}
	return c.args()
}
