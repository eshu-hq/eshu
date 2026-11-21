// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Shared staging collapse of the supply-chain impact winners dirty log
// (#10837 direction C: hot-key coalescing).
//
// Every capture owner (fact_upsert, fact_retract, the activation and
// scanner triggers, the audit queue, and scope-kind rows) plain-INSERTs one
// row per mark into supply_chain_impact_winners_dirty_staging (migration
// 309), which carries NO unique index: concurrent writers over the same hot
// keys never wait on a conflict arbiter. The maintainer tick collapses each
// wave into the versioned dirty log with one atomic key-ordered GROUP BY
// merge-upsert per kind plus the seq-bounded delete, inside the tick
// transaction under the resweep single-flight claim, before the tick-clock
// read. The collapse owns the only writes to the dirty log a tick makes
// before admission, and no writer touches the log anymore, so the merge
// meets no concurrent log writer; the key order is a determinism and
// plan-shape contract, pinned by the collapse live proof's EXPLAIN.
//
// Conservation: new-key collapse sets dirty_version to the staged mark
// count; the conflict arm adds the staged count to the queued version
// (never +1). Collapsed captured_at is the staged minimum per key and the
// source is the earliest staged seq's row per key; an existing queued row
// keeps both its captured_at and its source (first-marker-wins survives the
// collapse). One staged row is one mark, so the exact identity is marks ==
// drained version sums + residual log versions + staging residual.

// winnersStagingTable is the shared staging queue every capture owner
// appends to.
const winnersStagingTable = "supply_chain_impact_winners_dirty_staging"

// defaultWinnersStagingCollapseMaxRows bounds one tick's collapse: at most
// this many staged rows fold into the dirty log per tick. A full hot wave
// (162 keys x 150 marks = 24,300 rows) collapses in one tick; a larger
// backlog drains over successive ticks while the residual set keeps the
// staging-union horizon honest. It is a row budget, not a correctness
// bound: an unbounded collapse in the tick transaction is the landing
// blocker the adjudication forbids.
const defaultWinnersStagingCollapseMaxRows = 50000

// winnersStagingCollapseWorkMem is the work_mem the two collapse arms plan
// under (#11129): the fact arm's 50,000-row key sort takes about 11.7 MB in
// memory (measured on a 12.16M-row burst), so 32MB keeps it a quicksort with
// 2.7x headroom. It is set with set_config(..., true) for the arms only and
// the saved value is restored before the tick-clock read, so admission and
// apply plan under the session setting; a rollback discards it. A session
// already above it keeps its own value.
const winnersStagingCollapseWorkMem = "32MB"

// selectWinnersStagingCollapseBoundSQL reads the collapse budget from the seq
// index alone ($1 is maxRows): it walks at most maxRows+1 entries from the
// oldest staged seq and returns how many it saw (capped at maxRows+1), the
// lowest visible seq lo, and the bound, the maxRows-th seq or the newest one
// when fewer are staged. No statement in a tick reads more of the queue than
// the budget (#11129): the shipped count(*) and max(seq) read every staged
// row on every tick. lo comes from this unfloored read and is never stored:
// a row that commits below lo afterwards is not folded this tick and the next
// tick's read meets it first.
const selectWinnersStagingCollapseBoundSQL = `
SELECT count(*), COALESCE(min(seq), 0), COALESCE(max(seq) FILTER (WHERE n <= $1), 0)
FROM (SELECT seq, row_number() OVER (ORDER BY seq) AS n
      FROM supply_chain_impact_winners_dirty_staging
      ORDER BY seq LIMIT $1 + 1) AS head
`

// readWinnersStagingWorkMemSQL and setWinnersStagingWorkMemSQL scope
// work_mem to the collapse arms: the first reads the setting to restore, the
// second raises it transaction-locally to $1 unless the session already runs
// above it, or restores the saved value.
const (
	readWinnersStagingWorkMemSQL = `SELECT current_setting('work_mem')`
	setWinnersStagingWorkMemSQL  = `
SELECT set_config('work_mem',
    CASE WHEN pg_size_bytes(current_setting('work_mem')) < pg_size_bytes($1) THEN $1
         ELSE current_setting('work_mem') END, true)
`
	restoreWinnersStagingWorkMemSQL = `SELECT set_config('work_mem', $1, true)`
)

// collapseWinnersStagingFactSelectSQL is the fact arm's read-only SELECT:
// one row per staged fact key in the tick's seq range ($1 = lo, $2 = bound,
// #11129: the lower end keeps the index scan off the dead head a held
// snapshot leaves below the queue), carrying the
// conservation inputs per key (mark count for dirty_version += count, the
// staged minimum captured_at, the earliest staged seq's source). The SELECT
// feeds the merge below in conflict-key order. The collapse live proof
// EXPLAINs this arm instead of the INSERT, so the explained grouping and
// sort cannot drift from the grouping the tick runs (see
// convertWinnersKeyedScopeRemainderSelectSQL for the same derivation
// discipline).
const collapseWinnersStagingFactSelectSQL = `
SELECT package_id, repository_id, subject_digest, c0, advisory_id,
       count(*)::bigint AS marks, min(captured_at) AS first_mark,
       (array_agg(source ORDER BY seq))[1] AS first_source,
       array_agg(seq) AS seqs
FROM supply_chain_impact_winners_dirty_staging
WHERE kind = 'fact' AND seq >= $1 AND seq <= $2
GROUP BY package_id, repository_id, subject_digest, c0, advisory_id
`

// collapseWinnersStagingFactSQL folds the bound's staged fact rows into the
// versioned dirty log. The staged CTE is the one snapshot the merge and the
// delete share: rows are deleted exactly when they were merged, so a mark
// that commits between the two arms can never be deleted unmerged. The merge
// is key-ordered and upserts in the shipped version arithmetic; the delete
// removes exactly the staged seqs the merge read, oldest first by the seq
// bound. The delete carries the SELECT's seq range too (#11129): every seq in
// staged.seqs already satisfies it and the statement has one snapshot, so it
// changes no row, but without it the planner joins the unnested seqs against
// a Seq Scan of the whole queue (7.37 s with a 309 MB hash spill at a
// 12.16M-row burst). It returns the collapsed group count and the collapsed
// row count for the staging-collapsed counter.
var collapseWinnersStagingFactSQL = `
WITH staged AS (` + collapseWinnersStagingFactSelectSQL + `),
merged AS (
    INSERT INTO supply_chain_impact_winners_dirty_log
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at, dirty_version)
    SELECT 'fact', first_source, package_id, repository_id, subject_digest, c0, advisory_id, first_mark, marks
    FROM staged
    ORDER BY package_id, repository_id, subject_digest, c0, advisory_id
    ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'
    DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + EXCLUDED.dirty_version
    RETURNING 1
),
deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_staging AS s
    USING (SELECT unnest(seqs) AS seq FROM staged) AS u
    WHERE s.seq = u.seq AND s.seq >= $1 AND s.seq <= $2
    RETURNING 1
)
SELECT (SELECT count(*) FROM staged), (SELECT count(*) FROM deleted)
`

// collapseWinnersStagingScopeSelectSQL is the scope arm's read-only SELECT:
// the twin of the fact arm over the (scope_id, generation_id) pair.
const collapseWinnersStagingScopeSelectSQL = `
SELECT scope_id, generation_id,
       count(*)::bigint AS marks, min(captured_at) AS first_mark,
       (array_agg(source ORDER BY seq))[1] AS first_source,
       array_agg(seq) AS seqs
FROM supply_chain_impact_winners_dirty_staging
WHERE kind = 'scope' AND seq >= $1 AND seq <= $2
GROUP BY scope_id, generation_id
`

// collapseWinnersStagingScopeSQL folds the bound's staged scope rows into
// the versioned dirty log: the scope-twin of the fact arm above, key-ordered
// on the scope pair, upserting into the scope partial unique index with the
// same version arithmetic.
var collapseWinnersStagingScopeSQL = `
WITH staged AS (` + collapseWinnersStagingScopeSelectSQL + `),
merged AS (
    INSERT INTO supply_chain_impact_winners_dirty_log
        (kind, source, scope_id, generation_id, captured_at, dirty_version)
    SELECT 'scope', first_source, scope_id, generation_id, first_mark, marks
    FROM staged
    ORDER BY scope_id, generation_id
    ON CONFLICT (scope_id, generation_id) WHERE kind = 'scope'
    DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + EXCLUDED.dirty_version
    RETURNING 1
),
deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_staging AS s
    USING (SELECT unnest(seqs) AS seq FROM staged) AS u
    WHERE s.seq = u.seq AND s.seq >= $1 AND s.seq <= $2
    RETURNING 1
)
SELECT (SELECT count(*) FROM staged), (SELECT count(*) FROM deleted)
`

// winnersStagingExactCountCap is how far a residual read counts the staging
// queue exactly (#11129): up to the cap the count is exact; above it the read
// reports the visible seq span instead and marks it an upper bound. The cap is
// the collapse budget, so the read walks no more of the queue than a collapse
// does. Every consumer only needs "remaining > 0" exactly, which the bounded
// walk gives.
const winnersStagingExactCountCap = defaultWinnersStagingCollapseMaxRows

// selectWinnersStagingResidualSQL reads the staging queue's residual set in
// one snapshot ($1 is the exact-count cap): the count up to cap+1 and the
// lowest visible seq from one bounded seq-index walk, the newest seq from the
// index's right edge, and the exact oldest capture time from the captured_at
// index endpoint (migration 330). The minimum is never approximated: it feeds
// the stamp GREATEST(prev, LEAST(S, min - 1us)), and a value above the true
// minimum could stamp the watermark over an unapplied mark. No read filters
// on seq: a writer can commit a lower seq at any time, and captured_at is
// clock_timestamp() per row, so it is not monotone in seq.
const selectWinnersStagingResidualSQL = winnersStagingHeadCTE + `
SELECT ` + winnersStagingResidualColumns + `
`

// winnersStagingHeadCTE is the bounded seq-index walk ($1 is the exact-count
// cap) the residual read and the gauge sample share, and
// winnersStagingResidualColumns the four residual columns over it: the walked
// count, the lowest visible seq, the newest seq, and the exact oldest
// capture time.
const (
	winnersStagingHeadCTE = `
WITH head AS (
    SELECT seq FROM supply_chain_impact_winners_dirty_staging ORDER BY seq LIMIT $1 + 1
)`
	winnersStagingResidualColumns = `(SELECT count(*) FROM head),
       (SELECT min(seq) FROM head),
       (SELECT max(seq) FROM supply_chain_impact_winners_dirty_staging),
       (SELECT min(captured_at) FROM supply_chain_impact_winners_dirty_staging)`
)

// winnersStagingResidual is one residual read of the staging queue: the
// depth, whether the depth is an upper bound (the visible seq span past the
// exact-count cap), and the exact oldest capture time (invalid when empty).
type winnersStagingResidual struct {
	count      int64
	upperBound bool
	oldest     sql.NullTime
}

// winnersStagingCollapseOutcome is what one tick's collapse folded: the
// collapsed key-group count, the collapsed staged-row count the
// staging-collapsed counter records, and whether the per-tick row budget
// split the wave (the remainder stays staged and the tick stamps the
// staging-union horizon, not S).
type winnersStagingCollapseOutcome struct {
	groups int64
	rows   int64
	split  bool
}

// collapseWinnersStaging folds at most maxRows staged rows into the versioned
// dirty log and deletes exactly the rows it folded. A non-positive maxRows
// takes the default. An empty queue collapses nothing. The bound read walks
// at most maxRows+1 seq-index entries: when the queue holds more than the
// budget, the bound is the maxRows-th oldest seq and split reports the wave
// split; otherwise the bound is the newest staged seq. Both arms then fold
// the range [lo, bound].
func collapseWinnersStaging(ctx context.Context, tx db.Transaction, maxRows int64) (winnersStagingCollapseOutcome, error) {
	if maxRows <= 0 {
		maxRows = defaultWinnersStagingCollapseMaxRows
	}
	staged, lo, bound, err := readWinnersStagingCollapseBound(ctx, tx, maxRows)
	if err != nil || staged == 0 {
		return winnersStagingCollapseOutcome{}, err
	}
	outcome, err := foldWinnersStagingRange(ctx, tx, lo, bound)
	outcome.split = staged > maxRows
	return outcome, err
}

// readWinnersStagingCollapseBound runs the bounded bound read: the staged
// count it walked (at most maxRows+1), the lowest visible seq, and the bound.
func readWinnersStagingCollapseBound(ctx context.Context, tx db.Transaction, maxRows int64) (staged, lo, bound int64, err error) {
	err = scanWinnersStagingRow(ctx, tx, selectWinnersStagingCollapseBoundSQL, "read supply chain impact winners staging bound", []any{maxRows}, &staged, &lo, &bound)
	return staged, lo, bound, err
}

// foldWinnersStagingRange runs both collapse arms over the seq range
// [lo, bound] under a transaction-local work_mem and restores the saved
// value before it returns, so the statements after the collapse plan under
// the session setting. A row that committed below lo after the bound read,
// or inside the range after an arm's snapshot, is not folded and stays
// staged for the residual read and the next tick.
func foldWinnersStagingRange(ctx context.Context, tx db.Transaction, lo, bound int64) (winnersStagingCollapseOutcome, error) {
	var outcome winnersStagingCollapseOutcome
	saved, err := scanWinnersStagingText(ctx, tx, readWinnersStagingWorkMemSQL, "read supply chain impact winners collapse work_mem")
	if err != nil {
		return outcome, err
	}
	if _, err := scanWinnersStagingText(ctx, tx, setWinnersStagingWorkMemSQL, "scope supply chain impact winners collapse work_mem", winnersStagingCollapseWorkMem); err != nil {
		return outcome, err
	}
	for _, arm := range []struct {
		name string
		sql  string
	}{
		{"fact", collapseWinnersStagingFactSQL},
		{"scope", collapseWinnersStagingScopeSQL},
	} {
		var groups, rows int64
		if err := scanWinnersStagingRow(ctx, tx, arm.sql, "collapse supply chain impact winners staging "+arm.name+" rows", []any{lo, bound}, &groups, &rows); err != nil {
			return outcome, err
		}
		outcome.groups += groups
		outcome.rows += rows
	}
	if _, err := scanWinnersStagingText(ctx, tx, restoreWinnersStagingWorkMemSQL, "restore supply chain impact winners collapse work_mem", saved); err != nil {
		return outcome, err
	}
	return outcome, nil
}

// countWinnersStagingResidual reads the staging queue's residual set. It runs
// after the collapse, so every row it sees is post-bound or landed mid-tick
// and belongs in the tick's residual set. The depth is exact up to
// winnersStagingExactCountCap; past it the depth is the visible seq span
// max(seq) - min(seq) + 1, which no smaller set of distinct seqs can exceed
// (no writer sets seq; the sequence hands each row its own), so it is an
// upper bound and is marked one.
func countWinnersStagingResidual(ctx context.Context, q db.Queryer, what string) (winnersStagingResidual, error) {
	var residual winnersStagingResidual
	var lo, hi sql.NullInt64
	rows, err := q.QueryContext(ctx, selectWinnersStagingResidualSQL, int64(winnersStagingExactCountCap))
	if err != nil {
		return residual, fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		if err := rows.Scan(&residual.count, &lo, &hi, &residual.oldest); err != nil {
			return residual, fmt.Errorf("%s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return residual, fmt.Errorf("%s: %w", what, err)
	}
	if err := rows.Close(); err != nil {
		return residual, fmt.Errorf("close %s: %w", what, err)
	}
	residual.count, residual.upperBound = winnersStagingDepth(residual.count, lo, hi)
	return residual, nil
}

// winnersStagingDepth turns a bounded walk's count into the reported depth:
// the exact count when the walk stayed within the cap, else the visible seq
// span marked as an upper bound (never below the walked count).
func winnersStagingDepth(walked int64, lo, hi sql.NullInt64) (int64, bool) {
	if walked <= int64(winnersStagingExactCountCap) || !lo.Valid || !hi.Valid {
		return walked, false
	}
	span := hi.Int64 - lo.Int64 + 1
	if span < walked {
		span = walked
	}
	return span, true
}

// stagingUnionRemaining unions the dirty log's residual set with the staging
// queue's: the remaining depth is the sum, and the horizon minimum is the
// earlier of the two minima. An invalid minimum means its set is empty and
// loses to any valid one; two invalid minima stay invalid (the caller treats
// a non-empty remainder without a minimum as a bug, never as an empty log).
func stagingUnionRemaining(logRemaining int64, logMin sql.NullTime, staged winnersStagingResidual) (int64, sql.NullTime) {
	remaining := logRemaining + staged.count
	stagingMin := staged.oldest
	switch {
	case !stagingMin.Valid:
		return remaining, logMin
	case !logMin.Valid:
		return remaining, stagingMin
	case stagingMin.Time.Before(logMin.Time):
		return remaining, stagingMin
	default:
		return remaining, logMin
	}
}

// scanWinnersStagingRow runs a one-row staging statement with args, scans it
// into dest, and drains its cursor.
func scanWinnersStagingRow(ctx context.Context, q db.Queryer, query, what string, args []any, dest ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return fmt.Errorf("%s: statement returned no row", what)
	}
	if err := rows.Scan(dest...); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if rows.Next() {
		return fmt.Errorf("%s: statement returned more than one row", what)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s: %w", what, err)
	}
	return nil
}

// scanWinnersStagingText runs a one-row one-text staging statement.
func scanWinnersStagingText(ctx context.Context, q db.Queryer, query, what string, args ...any) (string, error) {
	var value string
	err := scanWinnersStagingRow(ctx, q, query, what, args, &value)
	return value, err
}

// DirtyLogStats reads the dirty log's depth and the age of its oldest row,
// unioned with the staging queue's (#10837 direction C), for the operator
// gauges. An empty log and queue read as zero depth and zero age. The staging
// part is the residual read's bounded walk (#11129): past the exact-count cap
// the staging depth, and so the unioned depth, is the visible seq span and
// StagingDepthUpperBound says so. Ages are taken against the database clock
// the statement returns, the same domain as captured_at.
func (s SupplyChainImpactWinnersStore) DirtyLogStats(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersDirtyStats, error) {
	var stats supplychaincore.SupplyChainImpactWinnersDirtyStats
	if s.DB == nil {
		return stats, fmt.Errorf("supply chain impact winners database is required")
	}
	var logDepth, walked int64
	var logMin, stagingMin sql.NullTime
	var lo, hi sql.NullInt64
	var now time.Time
	if err := scanWinnersStagingRow(ctx, s.DB, selectWinnersDirtyLogStatsSQL, "read supply chain impact winners dirty log stats",
		[]any{int64(winnersStagingExactCountCap)}, &logDepth, &logMin, &walked, &lo, &hi, &stagingMin, &now); err != nil {
		return stats, err
	}
	stats.StagingDepth, stats.StagingDepthUpperBound = winnersStagingDepth(walked, lo, hi)
	stats.Depth = logDepth + stats.StagingDepth
	if _, oldest := stagingUnionRemaining(0, logMin, winnersStagingResidual{oldest: stagingMin}); oldest.Valid {
		stats.OldestAge = now.Sub(oldest.Time)
	}
	if stagingMin.Valid {
		stats.StagingOldestAge = now.Sub(stagingMin.Time)
	}
	return stats, nil
}
