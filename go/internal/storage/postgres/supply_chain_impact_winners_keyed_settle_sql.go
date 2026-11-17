// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Versioned settle of the supply-chain impact winners dirty log (#10827 R2:
// Global Key-Ordered Versioned Upsert).
//
// The dirty log carries dirty_version (one row per pending recompute unit;
// every re-mark of a queued key bumps the version instead of appending a
// row). Every multi-row statement takes row locks in one total order over
// the conflict key — kind first, then the fact five-tuple, then the scope
// pair, with seq as the final tiebreak so pre-dedup duplicate keys still
// lock deterministically — so no two statements contend out of order. The
// order is per-statement, not per-transaction: a writer pass and a settle
// tick each run several ordered batches, and cross-batch inversion can
// still deadlock one victim into a rollback (#10851 tracks the
// transaction-global fix and its interleave gate). The naive versioned
// pattern deadlocked UPSERT-vs-DELETE at ~2.8e-6 per transaction under wave
// load; per-statement order plus the single-statement lock-then-delete
// below removes that within-statement cycle instead of retrying through it
// (no shipped deadlock retry anywhere; a retry firing in the gate is a
// fail).
//
// winnersDirtyKeyOrderClause renders that order for one alias. The writer
// upserts (fact capture, retract, triggers, audit queue) ORDER BY the same
// columns over the rows they insert; the settle drains below ORDER BY them
// over the admitted window before FOR UPDATE. Writers have no seq yet, so
// they order by the key alone; the settle tiebreak only separates identical
// keys, which writers serialize on the unique index instead.

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
// global order. Converted remainder rows carry a fresh conversion-time
// captured_at, so they can only raise a later horizon, never lower this one.
//
// convertWinnersKeyedScopeRemainderSelectSQL is the read-only SELECT arm of
// the convert below, key order included: plan proofs EXPLAIN this arm
// instead of the INSERT, so they measure the expansion-fan-out join and its
// sort without inserting. The shipped statement concatenates this arm, so
// the explained join cannot drift from the join the tick runs.
var convertWinnersKeyedScopeRemainderSelectSQL = `
SELECT 'fact', MIN(c.source), si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id
FROM ` + winnersKeyedConsumedTable + ` AS c
JOIN ` + winnersKeyedScopeIdentsTable + ` AS si
  ON si.scope_id = c.scope_id AND si.generation_id = c.generation_id
WHERE c.kind = 'scope'
  AND NOT ` + winnersKeyedChunkIdentMatch("si") + `
  AND EXISTS (
      SELECT 1 FROM ` + winnersKeyedScopeIdentsTable + ` AS applied
      WHERE applied.scope_id = c.scope_id AND applied.generation_id = c.generation_id
        AND ` + winnersKeyedChunkIdentMatch("applied") + `
  )
GROUP BY si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id
ORDER BY si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id
`

var convertWinnersKeyedScopeRemainderSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)` + convertWinnersKeyedScopeRemainderSelectSQL + `
ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'
DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + 1
`

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
      AND (
          (c.kind = 'fact' AND ` + winnersKeyedChunkIdentMatch("c") + `)
          OR (c.kind = 'scope' AND (
              NOT EXISTS (
                  SELECT 1 FROM ` + winnersKeyedScopeIdentsTable + ` AS si
                  WHERE si.scope_id = c.scope_id AND si.generation_id = c.generation_id
                    AND NOT ` + winnersKeyedChunkIdentMatch("si") + `
              )
              OR EXISTS (
                  SELECT 1 FROM ` + winnersKeyedScopeIdentsTable + ` AS applied
                  WHERE applied.scope_id = c.scope_id AND applied.generation_id = c.generation_id
                    AND ` + winnersKeyedChunkIdentMatch("applied") + `
              )
          ))
      )
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
