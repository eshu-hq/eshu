// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"

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

// selectWinnersStagingCollapseStatsSQL reads one row for the collapse
// budget: how many rows are staged, the newest staged seq, and the oldest
// staged capture time.
const selectWinnersStagingCollapseStatsSQL = `
SELECT count(*), COALESCE(max(seq), 0), min(captured_at)
FROM supply_chain_impact_winners_dirty_staging
`

// selectWinnersStagingCollapseBoundSQL reads the seq of the maxRows-th
// oldest staged row ($1 is maxRows - 1 as a 0-based offset): the collapse
// folds rows at or below this seq and leaves every newer row staged, so a
// cap that splits a wave never strands the remainder.
const selectWinnersStagingCollapseBoundSQL = `
SELECT seq FROM supply_chain_impact_winners_dirty_staging ORDER BY seq LIMIT 1 OFFSET $1
`

// collapseWinnersStagingFactSelectSQL is the fact arm's read-only SELECT:
// one row per staged fact key at or below the seq bound ($1), carrying the
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
WHERE kind = 'fact' AND seq <= $1
GROUP BY package_id, repository_id, subject_digest, c0, advisory_id
`

// collapseWinnersStagingFactSQL folds the bound's staged fact rows into the
// versioned dirty log. The staged CTE is the one snapshot the merge and the
// delete share: rows are deleted exactly when they were merged, so a mark
// that commits between the two arms can never be deleted unmerged. The merge
// is key-ordered and upserts in the shipped version arithmetic; the delete
// removes exactly the staged seqs the merge read, oldest first by the seq
// bound. It returns the collapsed group count and the collapsed row count
// for the staging-collapsed counter.
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
    WHERE s.seq = u.seq
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
WHERE kind = 'scope' AND seq <= $1
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
    WHERE s.seq = u.seq
    RETURNING 1
)
SELECT (SELECT count(*) FROM staged), (SELECT count(*) FROM deleted)
`

// selectWinnersStagingResidualSQL reads the staging queue's residual set:
// every row still staged after the collapse (post-bound arrivals plus rows
// that landed mid-tick). The tick unions it with the dirty log's residual
// set for the remaining count and the horizon minimum.
const selectWinnersStagingResidualSQL = `
SELECT count(*), min(captured_at) FROM supply_chain_impact_winners_dirty_staging
`

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
// takes the default. An empty queue collapses nothing. When the queue holds
// more than the budget, the bound is the maxRows-th oldest seq and split
// reports the wave split; otherwise the bound is the newest staged seq.
func collapseWinnersStaging(ctx context.Context, tx db.Transaction, maxRows int64) (winnersStagingCollapseOutcome, error) {
	var outcome winnersStagingCollapseOutcome
	if maxRows <= 0 {
		maxRows = defaultWinnersStagingCollapseMaxRows
	}
	var staged, maxSeq int64
	var oldest sql.NullTime
	if err := scanWinnersStagingRow(ctx, tx, selectWinnersStagingCollapseStatsSQL, "read supply chain impact winners staging stats", &staged, &maxSeq, &oldest); err != nil {
		return outcome, err
	}
	if staged == 0 {
		return outcome, nil
	}
	var bound int64
	if staged > maxRows {
		outcome.split = true
		if err := scanWinnersStagingSeq(ctx, tx, selectWinnersStagingCollapseBoundSQL, maxRows-1, &bound); err != nil {
			return outcome, err
		}
	} else {
		bound = maxSeq
	}
	for _, arm := range []struct {
		name string
		sql  string
	}{
		{"fact", collapseWinnersStagingFactSQL},
		{"scope", collapseWinnersStagingScopeSQL},
	} {
		var groups, rows int64
		if err := scanWinnersStagingCounts(ctx, tx, arm.sql, bound, &groups, &rows); err != nil {
			return outcome, fmt.Errorf("collapse supply chain impact winners staging %s rows: %w", arm.name, err)
		}
		outcome.groups += groups
		outcome.rows += rows
	}
	return outcome, nil
}

// countWinnersStagingResidual reads the staging queue's residual row count
// and oldest capture time. It runs after the collapse, so every row it sees
// is post-bound or landed mid-tick and belongs in the tick's residual set.
func countWinnersStagingResidual(ctx context.Context, q db.Queryer, what string) (int64, sql.NullTime, error) {
	rows, err := q.QueryContext(ctx, selectWinnersStagingResidualSQL)
	if err != nil {
		return 0, sql.NullTime{}, fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	var count int64
	var oldest sql.NullTime
	if rows.Next() {
		if err := rows.Scan(&count, &oldest); err != nil {
			return 0, sql.NullTime{}, fmt.Errorf("%s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, sql.NullTime{}, fmt.Errorf("%s: %w", what, err)
	}
	if err := rows.Close(); err != nil {
		return 0, sql.NullTime{}, fmt.Errorf("close %s: %w", what, err)
	}
	return count, oldest, nil
}

// stagingUnionRemaining unions the dirty log's residual set with the staging
// queue's: the remaining depth is the sum, and the horizon minimum is the
// earlier of the two minima. An invalid minimum means its set is empty and
// loses to any valid one; two invalid minima stay invalid (the caller treats
// a non-empty remainder without a minimum as a bug, never as an empty log).
func stagingUnionRemaining(logRemaining int64, logMin sql.NullTime, stagingCount int64, stagingMin sql.NullTime) (int64, sql.NullTime) {
	remaining := logRemaining + stagingCount
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

// scanWinnersStagingRow runs a one-row (count, seq, timestamptz) staging
// query and drains its cursor.
func scanWinnersStagingRow(ctx context.Context, q db.Queryer, query, what string, count, seq *int64, oldest *sql.NullTime) error {
	rows, err := q.QueryContext(ctx, query)
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
	var n int64
	var s int64
	var t sql.NullTime
	if err := rows.Scan(&n, &s, &t); err != nil {
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
	*count = n
	*seq = s
	*oldest = t
	return nil
}

// scanWinnersStagingSeq runs a one-row one-seq query and drains its cursor.
func scanWinnersStagingSeq(ctx context.Context, q db.Queryer, query string, offset int64, seq *int64) error {
	rows, err := q.QueryContext(ctx, query, offset)
	if err != nil {
		return fmt.Errorf("read supply chain impact winners staging bound: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read supply chain impact winners staging bound: %w", err)
		}
		return fmt.Errorf("read supply chain impact winners staging bound: statement returned no row")
	}
	if err := rows.Scan(seq); err != nil {
		return fmt.Errorf("read supply chain impact winners staging bound: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read supply chain impact winners staging bound: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close supply chain impact winners staging bound: %w", err)
	}
	return nil
}

// scanWinnersStagingCounts runs one collapse arm at the seq bound and reads
// its (groups, collapsed rows) counts, draining its cursor.
func scanWinnersStagingCounts(ctx context.Context, q db.Queryer, query string, bound int64, groups, collapsed *int64) error {
	rows, err := q.QueryContext(ctx, query, bound)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("collapse arm returned no counts")
	}
	if err := rows.Scan(groups, collapsed); err != nil {
		return err
	}
	if rows.Next() {
		return fmt.Errorf("collapse arm returned more than one counts row")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return rows.Close()
}
