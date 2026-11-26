// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Versioned settle of the supply-chain impact winners dirty log (#10827 R2:
// Global Key-Ordered Versioned Upsert, #10851 transaction-global).
//
// The dirty log carries dirty_version (one row per pending recompute unit;
// every re-mark of a queued key bumps the version instead of appending a
// row). No writer touches the log anymore: every capture owner stages plain
// INSERTs into the queue (#10837 direction C, no unique index, no row
// locks), and the maintainer tick's collapse is the only log write a tick
// makes before admission, running inside the tick transaction under the
// resweep single-flight claim. The collapse's two merge-upserts and the
// convert below therefore meet no concurrent log writer, so the old
// cross-batch inversion is gone by construction — the fix is
// transaction-global, not per-statement. The settle drain still takes its
// row locks in one total order over the conflict key — kind first, then the
// fact five-tuple, then the scope pair, with seq as the final tiebreak — in
// its single lock-then-delete statement, and the naive versioned pattern's
// UPSERT-vs-DELETE cycle stays removed instead of retried through (no
// shipped deadlock retry anywhere; a retry firing in the gate is a fail).
// The writer-txn replay and the wave-load gates pin the runtime behavior:
// zero 40P01 with no retry and exact conservation.
//
// winnersDirtyKeyOrderClause renders that order for one alias. The tick's own
// log writers — the collapse's two merge-upserts and the scope convert —
// ORDER BY the same columns over the rows they merge, and the settle drains
// below ORDER BY them over the admitted window before FOR UPDATE. Writers
// stage with no ORDER BY (no locks to take in order); the settle tiebreak
// only separates identical keys.

// winnersDirtyKeyOrderClause renders the R2 total lock order over the dirty
// log's conflict key for the rows of alias: kind, the fact five-tuple, the
// scope pair, then seq. NULLs sort last under the default ASC order, which
// is deterministic; fact rows carry NULL scope columns and scope rows NULL
// fact columns, so the two kinds never interleave ambiguously. Callers place
// it as the ORDER BY of a locking SELECT immediately before FOR UPDATE: the
// LockRows node sits above the Sort, so locks are taken in this order
// (pinned by the deadlock live proof's EXPLAIN assertion).
func winnersDirtyKeyOrderClause(alias string) string {
	return alias + `.kind, ` +
		alias + `.package_id, ` +
		alias + `.repository_id, ` +
		alias + `.subject_digest, ` +
		alias + `.c0, ` +
		alias + `.advisory_id, ` +
		alias + `.scope_id, ` +
		alias + `.generation_id, ` +
		alias + `.seq`
}

// convertWinnersKeyedScopeRemainderSQL turns the unapplied remainder of a
// partially applied scope row into fact dirty rows, so the scope row can
// leave the log and the next tick resumes from named keys, not from
// re-expanding the whole generation and re-applying its prefix. One
// identifier can be the remainder of two scope rows with different sources;
// the SELECT groups by the conflict key (MIN(source) picks deterministically)
// because one statement may propose each key only once — two proposed rows
// for one key make Postgres refuse the whole upsert.
// A scope row with no applied identifier is left alone (nothing to resume from;
// it re-expands next tick), and a fully applied one converts nothing.
//
// R2: the conversion is a K-ordered upsert. A concurrent writer may have
// marked the same key while the tick ran, so a plain INSERT could violate
// the versioned unique indexes; ON CONFLICT bumps the version instead and
// the SELECT's key order keeps the conversion's lock acquisition in the
// global order.
//
// #11145: a converted remainder row carries the captured_at of the scope row
// it came from (MIN over the scope rows that name the key), and a conflict
// with a queued fact row keeps the earlier of the two (LEAST). The horizon is
// the residual set's earliest capture minus 1us, so the watermark stays under
// the scope mark until every identifier of that activation is applied. A
// fresh conversion-time capture would let the horizon pass the scope mark
// with remainder winners still stale, and the in-transaction
// winners_watermark wake would reopen a waiter that depends on the
// activation too early. Carrying the earlier capture also keeps the
// remainder's age in the oldest-first identifier chunk, and since #11075 in
// window admission too: admission orders by (captured_at, seq), so the
// converted row is admitted by its carried age even though it takes a fresh
// tail seq. It also keeps dirty_log_oldest_age_seconds reporting the
// activation's real age.
//
// convertWinnersKeyedScopeRemainderSelectSQL is the read-only SELECT arm of
// the convert below, key order included: plan proofs EXPLAIN this arm
// instead of the INSERT, so they measure the expansion-fan-out join and its
// sort without inserting. The shipped statement concatenates this arm, so
// the explained join cannot drift from the join the tick runs.
//
// The scope-applied test is a once-per-tick MATERIALIZED set, probed by
// (scope_id, generation_id): the chunk join runs once over the expansion
// instead of once per expansion row. A correlated applied-scope EXISTS lets
// the planner pick the nested-loop shape that re-probes the chunk per
// expansion row (41,400 outer loops firing one probe per same-scope applied
// row, #10839), so the set stays an explicit MATERIALIZED CTE the planner
// cannot inline. The NOT-chunk-match remainder predicate is unchanged.
var convertWinnersKeyedScopeRemainderSelectSQL = `
WITH chunk_applied AS MATERIALIZED (
    SELECT si.scope_id, si.generation_id
    FROM ` + winnersKeyedScopeIdentsTable + ` AS si
    JOIN ` + winnersKeyedChunkIdentsTable + ` AS chunk
      ON (chunk.package_id, chunk.repository_id, chunk.subject_digest, chunk.c0, chunk.advisory_id)
       = (si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id)
)
SELECT 'fact', MIN(c.source), si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id, MIN(c.captured_at)
FROM ` + winnersKeyedConsumedTable + ` AS c
JOIN ` + winnersKeyedScopeIdentsTable + ` AS si
  ON si.scope_id = c.scope_id AND si.generation_id = c.generation_id
WHERE c.kind = 'scope'
  AND NOT ` + winnersKeyedChunkIdentMatch("si") + `
  AND EXISTS (
    SELECT 1 FROM chunk_applied AS ca
    WHERE ca.scope_id = c.scope_id AND ca.generation_id = c.generation_id
)
GROUP BY si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id
ORDER BY si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id
`

var convertWinnersKeyedScopeRemainderSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)` + convertWinnersKeyedScopeRemainderSelectSQL + `
ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'
DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + 1,
    captured_at = LEAST(supply_chain_impact_winners_dirty_log.captured_at, EXCLUDED.captured_at)
`

// convertWinnersKeyedScopeRemainder runs convertWinnersKeyedScopeRemainderSQL
// in the tick transaction and returns the fact rows it wrote, inserted or
// merged into a queued row (#11145: the scope_remainder_converted_rows_total
// signal). A driver that cannot report affected rows yields 0, never an
// error: the count is telemetry, and the conversion itself already
// succeeded.
func convertWinnersKeyedScopeRemainder(ctx context.Context, tx db.Transaction) (int64, error) {
	result, err := tx.ExecContext(ctx, convertWinnersKeyedScopeRemainderSQL)
	if err != nil {
		return 0, fmt.Errorf("convert supply chain impact winners scope remainder: %w", err)
	}
	if result == nil {
		return 0, nil
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return n, nil
}

// winnersKeyedAppliedPredicate renders the applied-row predicate over an
// admitted-window alias: a fact row whose identifier the chunk applies, or a
// scope row whose expansion has no unapplied identifier left (fully applied,
// or partially applied with its remainder converted, which queues every
// unapplied identifier as a fact row). It is the single source the applied
// drain and both settle skip counts read, so the per-tick admitted-vs-deleted
// split (#9975 drain discriminator) cannot drift from what the drain deletes.
func winnersKeyedAppliedPredicate(consumed string) string {
	return `(
          (` + consumed + `.kind = 'fact' AND ` + winnersKeyedChunkIdentMatch(consumed) + `)
          OR (` + consumed + `.kind = 'scope' AND (
              NOT EXISTS (
                  SELECT 1 FROM ` + winnersKeyedScopeIdentsTable + ` AS si
                  WHERE si.scope_id = ` + consumed + `.scope_id AND si.generation_id = ` + consumed + `.generation_id
                    AND NOT ` + winnersKeyedChunkIdentMatch("si") + `
              )
              OR EXISTS (
                  SELECT 1 FROM ` + winnersKeyedScopeIdentsTable + ` AS applied
                  WHERE applied.scope_id = ` + consumed + `.scope_id AND applied.generation_id = ` + consumed + `.generation_id
                    AND ` + winnersKeyedChunkIdentMatch("applied") + `
              )
          ))
      )`
}

// deleteWinnersKeyedAppliedDirtySQL deletes exactly the admitted dirty rows
// this chunk applied, in one lock-then-delete statement, and tallies them by
// capture source: a fact row whose identifier was probed, and a scope row
// whose expansion has no unapplied identifier left — fully applied, or
// partially applied with its remainder converted above (after conversion
// every unapplied identifier is queued as a fact row, so the scope row's
// information survives its deletion). Scope rows with nothing applied and
// fact rows of unapplied identifiers stay queued. winnersDirtyTallyCTEs adds
// the NVD trip tally.
//
// R2 lock-then-delete: the locked CTE joins the admitted window to the log,
// keeps only rows whose version still equals the admitted version, and locks
// them in the global key order with FOR UPDATE. A mark committed for a key
// after the tick admitted it bumps the version past the admitted snapshot,
// so the recheck leaves that row queued and the next tick applies it; without
// the recheck the tick would delete a mark it never applied. The delete then
// removes only the locked seqs. The tally sums dirty_version, so one consumed
// row counts every mark it collapsed, and the NVD trip CTEs sum versions the
// same way.
var deleteWinnersKeyedAppliedDirtySQL = `
WITH locked AS (
    SELECT d.seq AS seq
    FROM supply_chain_impact_winners_dirty_log AS d
    JOIN ` + winnersKeyedConsumedTable + ` AS c ON c.seq = d.seq
    WHERE d.dirty_version = c.dirty_version
      AND ` + winnersKeyedAppliedPredicate("c") + `
    ORDER BY ` + winnersDirtyKeyOrderClause("c") + `
    FOR UPDATE OF d
),
deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_log AS d
    USING locked AS l
    WHERE d.seq = l.seq
    RETURNING d.source, d.dirty_version, d.kind, d.captured_at, d.advisory_id, d.scope_id
)` + winnersDirtyTallyCTEs + `
SELECT source, sum(dirty_version)::bigint FROM deleted GROUP BY source
`

// countWinnersKeyedSettleSkippedNotInChunkSQL counts admitted rows the
// applied drain left queued because the chunk did not apply them (#9975
// drain discriminator): admitted survivors of the delete whose identifier
// fails the shared applied predicate. It is a read-only SELECT over the
// admitted window and the log's primary key; it takes no row locks. The
// over-cap fallback never runs it: that path builds no chunk table.
var countWinnersKeyedSettleSkippedNotInChunkSQL = `
SELECT count(*) FROM ` + winnersKeyedConsumedTable + ` AS c
JOIN supply_chain_impact_winners_dirty_log AS d ON d.seq = c.seq
WHERE NOT ` + winnersKeyedAppliedPredicate("c") + `
`

// countWinnersKeyedSettleSkippedVersionMismatchSQL counts admitted rows the
// applied drain left queued inside the chunk (#9975 drain discriminator):
// admitted survivors of the delete whose identifier passes the shared
// applied predicate, so only the version recheck can have kept them — a
// mark committed for the key after admission (the tick's own scope-remainder
// convert) bumped the log past the admitted snapshot. Same read-only shape
// as the not-in-chunk count; never run on the over-cap fallback path.
var countWinnersKeyedSettleSkippedVersionMismatchSQL = `
SELECT count(*) FROM ` + winnersKeyedConsumedTable + ` AS c
JOIN supply_chain_impact_winners_dirty_log AS d ON d.seq = c.seq
WHERE ` + winnersKeyedAppliedPredicate("c") + `
`

// deleteWinnersKeyedAllDirtySQL is the over-cap fallback's delete (var, not
// const: the key order renders through winnersDirtyKeyOrderClause): the full
// rebuild recomputes every key, so every ADMITTED row is consumed, tallied
// by source and, like the applied delete, by the NVD trip tally. It is the
// same lock-then-delete shape as the applied drain — key-ordered FOR UPDATE
// with the version recheck — minus the applied predicate: rows that arrived
// after admission were never admitted and stay queued for the next tick,
// which re-ticks immediately while Bounded. Stamping S over them would cover
// rows the rebuild may not have seen, so the caller stamps the horizon when
// the residual set is non-empty.
var deleteWinnersKeyedAllDirtySQL = `
WITH locked AS (
    SELECT d.seq AS seq
    FROM supply_chain_impact_winners_dirty_log AS d
    JOIN ` + winnersKeyedConsumedTable + ` AS c ON c.seq = d.seq
    WHERE d.dirty_version = c.dirty_version
    ORDER BY ` + winnersDirtyKeyOrderClause("c") + `
    FOR UPDATE OF d
),
deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_log AS d
    USING locked AS l
    WHERE d.seq = l.seq
    RETURNING d.source, d.dirty_version, d.kind, d.captured_at, d.advisory_id, d.scope_id
)` + winnersDirtyTallyCTEs + `
SELECT source, sum(dirty_version)::bigint FROM deleted GROUP BY source
`

// drainWinnersDirtyLogForFullSQL deletes every dirty-log row the statement
// sees, for the full resweep (#11117), and tallies the deleted marks by
// capture source and by the NVD trip tally, like the keyed drains. It keeps
// their lock-then-delete shape: the locked CTE takes the rows' locks in the
// global key order (winnersDirtyKeyOrderClause) with FOR UPDATE, and the
// delete removes exactly the locked seqs. There is no admission copy to
// recheck a version against: the lock and the delete are one statement, and a
// row whose version a committed tick bumped is re-read under the lock with
// its bumped version, a change the later rebuild snapshot also sees. The tick
// collapse and convert are the only other log writers and run under the same
// resweep lock, so the drain meets no concurrent log writer.
var drainWinnersDirtyLogForFullSQL = `
WITH locked AS (
    SELECT d.seq AS seq
    FROM supply_chain_impact_winners_dirty_log AS d
    ORDER BY ` + winnersDirtyKeyOrderClause("d") + `
    FOR UPDATE OF d
),
deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_log AS d
    USING locked AS l
    WHERE d.seq = l.seq
    RETURNING d.source, d.dirty_version, d.kind, d.captured_at, d.advisory_id, d.scope_id
)` + winnersDirtyTallyCTEs + `
SELECT source, sum(dirty_version)::bigint FROM deleted GROUP BY source
`

// drainWinnersStagingForFullSQL deletes every staged mark the statement sees,
// for the full resweep (#11117), and tallies them like the log drain. One
// staged row is one mark, so each counts as dirty_version 1: the same sum the
// collapse would have folded into the log. Writers only INSERT staged rows
// and take no row locks, so the delete waits on none; a staged row whose
// writer has not committed is invisible to the delete and stays queued.
var drainWinnersStagingForFullSQL = `
WITH deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_staging AS s
    RETURNING s.source, 1::bigint AS dirty_version, s.kind, s.captured_at, s.advisory_id, s.scope_id
)` + winnersDirtyTallyCTEs + `
SELECT source, sum(dirty_version)::bigint FROM deleted GROUP BY source
`
