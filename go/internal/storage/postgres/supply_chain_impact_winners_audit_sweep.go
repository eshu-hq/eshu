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

// Writer SQL of the staged winners audit sweep (#11131): the persisted cursor
// and the per-range verify. One range commits in one writer transaction: the
// pending filter, the verify and its repairs, and the compare-and-set of the
// cursor. A lost compare-and-set rolls the repairs back.

// WinnersAuditRangeLockTimeout bounds the waiting resweep-lock claim a range
// verify falls back to after two try-lock misses on the same range. It is
// lock_timeout, not a statement timeout: a keyed tick that holds the lock
// longer makes the claim fail with SQLSTATE 55P03, recorded as lock_busy,
// and the range retries. The value is provisional until the measured keyed
// tick maximum (theory measurement M8 of the #11131 ruling) replaces it.
const WinnersAuditRangeLockTimeout = 20 * time.Second

// winnersAuditSweepTable is the singleton cursor row (migration 335).
const winnersAuditSweepTable = "supply_chain_impact_winners_audit_sweep"

// winnersAuditRangePendingTable holds the candidates already queued, found
// before the lock is taken. The CREATE spells the name out for the DDL parity
// guard; TestWinnersTempTableDDLNamesMatchTheTableConstants pins both.
const winnersAuditRangePendingTable = "winners_audit_range_pending"

// selectWinnersAuditSweepSQL reads the cursor row, creating it on first use.
// The CTE's insert is invisible to the outer read, so the UNION returns one
// row whether the insert ran or the row already existed.
const selectWinnersAuditSweepSQL = `
WITH created AS (
    INSERT INTO ` + winnersAuditSweepTable + ` (singleton) VALUES (1)
    ON CONFLICT (singleton) DO NOTHING
    RETURNING sweep_id, cursor_package_id, cursor_repository_id, cursor_subject_digest, cursor_c0, cursor_advisory_id,
        ranges_done, sweep_started_at, last_sweep_completed_at, last_sweep_seconds, last_sweep_ranges,
        last_sweep_facts, last_sweep_candidates, last_sweep_repairs, last_sweep_discards
)
SELECT * FROM created
UNION ALL
SELECT sweep_id, cursor_package_id, cursor_repository_id, cursor_subject_digest, cursor_c0, cursor_advisory_id,
    ranges_done, sweep_started_at, last_sweep_completed_at, last_sweep_seconds, last_sweep_ranges,
    last_sweep_facts, last_sweep_candidates, last_sweep_repairs, last_sweep_discards
FROM ` + winnersAuditSweepTable + `
WHERE singleton = 1`

// winnersAuditRestrictedPendingSelect returns the identifiers of table (five
// columns) that are already waiting to be consumed: a dirty-log or staging
// fact row with the same identifier, or a fact of the identifier in a
// (scope, generation) a queued scope row names. It is the set
// createWinnersAuditPendingSQL builds, restricted to table's identifiers, so
// a range verify never expands the whole queue: the fact arm probes the
// dirty log's unique fact-key index, the scope arm probes the keyed winners
// index once per identifier, and staging (which has no identifier index) is
// read by one hash join against the small identifier set.
func winnersAuditRestrictedPendingSelect(table string) string {
	ident := `(i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id)`
	return `SELECT i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id
FROM ` + table + ` AS i
JOIN supply_chain_impact_winners_dirty_log AS d
  ON d.kind = 'fact'
 AND (d.package_id, d.repository_id, d.subject_digest, d.c0, d.advisory_id) = ` + ident + `
UNION
SELECT i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id
FROM ` + table + ` AS i
JOIN ` + winnersStagingTable + ` AS s
  ON s.kind = 'fact'
 AND (s.package_id, s.repository_id, s.subject_digest, s.c0, s.advisory_id) = ` + ident + `
UNION
SELECT i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id
FROM ` + table + ` AS i
CROSS JOIN LATERAL (
    SELECT f.scope_id, f.generation_id
    FROM fact_records AS f
    WHERE (f.payload->>'package_id') = i.package_id
      AND (f.payload->>'repository_id') = i.repository_id
      AND (f.payload->>'subject_digest') = i.subject_digest
      AND COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '') = i.c0
      AND COALESCE(f.payload->>'advisory_id', '') = i.advisory_id
      AND f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE
    OFFSET 0
) AS f
JOIN (
    SELECT scope_id, generation_id FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope'
    UNION
    SELECT scope_id, generation_id FROM ` + winnersStagingTable + ` WHERE kind = 'scope'
) AS p
  ON p.scope_id = f.scope_id
 AND p.generation_id = f.generation_id`
}

// createWinnersAuditRangePendingSQL finds, before any lock, the range
// candidates a keyed tick will recompute anyway.
var createWinnersAuditRangePendingSQL = `
CREATE TEMP TABLE winners_audit_range_pending ON COMMIT DROP AS
` + winnersAuditRestrictedPendingSelect(winnersKeyedIdentsTable)

// dropWinnersAuditRangePendingIdentsSQL removes those candidates from the
// identifier table the verify probes, so the lock is taken only when a
// candidate that nothing has queued remains.
const dropWinnersAuditRangePendingIdentsSQL = `
DELETE FROM ` + winnersKeyedIdentsTable + ` AS i
USING ` + winnersAuditRangePendingTable + ` AS p
WHERE (i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id)
    = (p.package_id, p.repository_id, p.subject_digest, p.c0, p.advisory_id)`

// countWinnersAuditRangeRemainingSQL counts the candidates left to verify.
const countWinnersAuditRangeRemainingSQL = `SELECT count(*) FROM ` + winnersKeyedIdentsTable

// createWinnersAuditRangeStalePendingSQL rechecks, under the lock, which of
// the verified stale identifiers were queued since the pre-lock filter. The
// table carries the name queueWinnersAuditRepairsSQL reads.
var createWinnersAuditRangeStalePendingSQL = `
CREATE TEMP TABLE winners_audit_pending ON COMMIT DROP AS
` + winnersAuditRestrictedPendingSelect(winnersAuditStaleTable)

// setWinnersAuditRangeLockTimeoutSQL scopes the waiting claim's bound to the
// range transaction.
var setWinnersAuditRangeLockTimeoutSQL = fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", WinnersAuditRangeLockTimeout.Milliseconds())

// waitClaimSupplyChainImpactWinnersResweepSQL is the waiting form of the
// resweep lock claim, on the same key as tryClaimSupplyChainImpactWinnersResweepSQL.
// PostgreSQL grants a released lock to a queued waiter before a later
// try-lock can take it, so a verify queued here wins the next release.
const waitClaimSupplyChainImpactWinnersResweepSQL = `
SELECT pg_advisory_xact_lock(hashtext('supply_chain_impact_winners'), hashtext('resweep'))
`

// advanceWinnersAuditSweepSQL is the compare-and-set of the cursor. It moves
// the cursor from $2..$6 (NULL at sweep start) to $7..$11 (NULL when the
// range was the last) only while sweep_id is still $1 and the stored cursor
// still equals $2..$6. Zero rows means another instance advanced this range
// first: the caller rolls its repairs back. $12 ends the sweep (the last
// range); $13 also records it as completed (the end NULL probe read clean).
// $14..$17 are the range's facts, candidates, repairs and discards.
const advanceWinnersAuditSweepSQL = `
UPDATE ` + winnersAuditSweepTable + ` SET
    sweep_id = CASE WHEN $12::boolean THEN sweep_id + 1 ELSE sweep_id END,
    cursor_package_id = $7::text,
    cursor_repository_id = $8::text,
    cursor_subject_digest = $9::text,
    cursor_c0 = $10::text,
    cursor_advisory_id = $11::text,
    sweep_started_at = CASE WHEN $12::boolean THEN NULL ELSE COALESCE(sweep_started_at, clock_timestamp()) END,
    ranges_done = CASE WHEN $12::boolean THEN 0 ELSE ranges_done + 1 END,
    sweep_facts = CASE WHEN $12::boolean THEN 0 ELSE sweep_facts + $14::bigint END,
    sweep_candidates = CASE WHEN $12::boolean THEN 0 ELSE sweep_candidates + $15::bigint END,
    sweep_repairs = CASE WHEN $12::boolean THEN 0 ELSE sweep_repairs + $16::bigint END,
    sweep_discards = CASE WHEN $12::boolean THEN 0 ELSE sweep_discards + $17::bigint END,
    last_sweep_completed_at = CASE WHEN $13::boolean THEN clock_timestamp() ELSE last_sweep_completed_at END,
    last_sweep_seconds = CASE WHEN $13::boolean
        THEN EXTRACT(EPOCH FROM clock_timestamp() - COALESCE(sweep_started_at, clock_timestamp()))::double precision
        ELSE last_sweep_seconds END,
    last_sweep_ranges = CASE WHEN $13::boolean THEN ranges_done + 1 ELSE last_sweep_ranges END,
    last_sweep_facts = CASE WHEN $13::boolean THEN sweep_facts + $14::bigint ELSE last_sweep_facts END,
    last_sweep_candidates = CASE WHEN $13::boolean THEN sweep_candidates + $15::bigint ELSE last_sweep_candidates END,
    last_sweep_repairs = CASE WHEN $13::boolean THEN sweep_repairs + $16::bigint ELSE last_sweep_repairs END,
    last_sweep_discards = CASE WHEN $13::boolean THEN sweep_discards + $17::bigint ELSE last_sweep_discards END,
    updated_at = clock_timestamp()
WHERE singleton = 1
  AND sweep_id = $1::bigint
  AND ROW(cursor_package_id, cursor_repository_id, cursor_subject_digest, cursor_c0, cursor_advisory_id)
      IS NOT DISTINCT FROM ROW($2::text, $3::text, $4::text, $5::text, $6::text)
RETURNING sweep_id, ranges_done, last_sweep_completed_at, last_sweep_seconds`

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
	// NoLock forbids the resweep lock: the keyed-yield rule after a locked
	// verify (#11131). A range that still has a candidate nothing has queued
	// rolls back with Yielded; one with none advances without the lock.
	NoLock bool
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
	// Yielded reports a NoLock verify that needed the lock; nothing
	// committed.
	Yielded bool
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
		locked, skip, yield, err := s.verifyWinnersAuditRangeCandidates(ctx, tx, v, &out)
		if err != nil {
			if isLockTimeoutError(err) {
				return WinnersAuditRangeOutcome{LockBusy: true, Candidates: out.Candidates}, nil
			}
			return none, err
		}
		if skip {
			return WinnersAuditRangeOutcome{Skipped: true, Candidates: out.Candidates}, nil
		}
		if yield {
			return WinnersAuditRangeOutcome{Yielded: true, Candidates: out.Candidates, Pending: out.Pending}, nil
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
// transaction. It reports whether it took the lock, whether the try-lock
// lost (skip), and whether a NoLock verify needed the lock (yield).
func (s SupplyChainImpactWinnersStore) verifyWinnersAuditRangeCandidates(
	ctx context.Context, tx db.Transaction, v WinnersAuditRangeVerify, out *WinnersAuditRangeOutcome,
) (locked, skip, yield bool, err error) {
	if _, err := tx.ExecContext(ctx, createWinnersAuditStaleIdentsSQL); err != nil {
		return false, false, false, fmt.Errorf("seed winners audit range candidates: %w", err)
	}
	if err := seedWinnersAuditStaleIdents(ctx, tx, v.Range.Candidates); err != nil {
		return false, false, false, err
	}
	for _, stmt := range []string{analyzeWinnersKeyedIdentsSQL, createWinnersAuditRangePendingSQL, dropWinnersAuditRangePendingIdentsSQL} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return false, false, false, fmt.Errorf("filter winners audit range candidates: %w", err)
		}
	}
	remaining, err := scanWinnersInt64(ctx, tx, countWinnersAuditRangeRemainingSQL, "count winners audit range candidates")
	if err != nil {
		return false, false, false, err
	}
	out.Pending = out.Candidates - remaining
	if remaining == 0 {
		return false, false, false, nil
	}
	if v.NoLock {
		return false, false, true, nil
	}
	if v.WaitForLock {
		if _, err := tx.ExecContext(ctx, setWinnersAuditRangeLockTimeoutSQL); err != nil {
			return false, false, false, fmt.Errorf("bound winners audit range lock wait: %w", err)
		}
		if _, err := tx.ExecContext(ctx, waitClaimSupplyChainImpactWinnersResweepSQL); err != nil {
			return false, false, false, fmt.Errorf("claim supply chain impact winners resweep (waiting): %w", err)
		}
	} else {
		claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx)
		if err != nil {
			return false, false, false, err
		}
		if !claimed {
			return false, true, false, nil
		}
	}
	for _, stmt := range []string{
		analyzeWinnersKeyedIdentsSQL,
		buildWinnersKeyedActiveFactsSQL, addWinnersKeyedOperatorFactsSQL, analyzeWinnersKeyedActiveFactsSQL,
		createWinnersAuditStaleSQL, createWinnersAuditRangeStalePendingSQL,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return true, false, false, fmt.Errorf("verify winners audit range candidates: %w", err)
		}
	}
	queued, err := tx.ExecContext(ctx, queueWinnersAuditRepairsSQL)
	if err != nil {
		return true, false, false, fmt.Errorf("queue winners audit range repairs: %w", err)
	}
	if out.Repairs, err = queued.RowsAffected(); err != nil {
		return true, false, false, fmt.Errorf("read winners audit range repairs: %w", err)
	}
	return true, false, false, nil
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
