// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// SQL of the chunked consume (#9805). The arbiter ruling behind this lane is
// chunk-build-sufficient: the keyed apply scales per changed/deleted row in
// the identifier set (no full-table rewrite in keyed apply), so bounding the
// post-expansion identifier set per tick bounds the apply, with no separate
// apply bound. The bound therefore applies to post-expansion identifiers,
// never to raw dirty rows: the admitted dirty-row window below only limits
// the candidates the expansion reads, and the truncation decision is made on
// identifiers.
//
// The chunk is a prefix of the ordered post-expansion identifier set. The
// active_facts build is the shipped per-identifier probe over the 291
// five-tuple keyed index (buildWinnersKeyedActiveFactsSQL, unchanged): with
// all five equalities as index conditions the heap fetches only the facts a
// dirty key keeps, so no candidate-row budget is needed. The lane this file
// replaced probed per package triple over the triple-only package-lookup
// index; migration 293 retired that index, so per-triple probing has no
// access path anymore.

// selectWinnersKeyedChunkIdentsSQL lists the identifier set's five-tuples in
// a fixed order, $1 of them. The caller asks for one past the bound, so a
// full page proves the set continues past this chunk. The first identifier
// always completes, so even a bound of one drains.
const selectWinnersKeyedChunkIdentsSQL = `
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM ` + winnersKeyedIdentsTable + `
ORDER BY package_id, repository_id, subject_digest, c0, advisory_id
LIMIT $1
`

// createWinnersKeyedChunkIdentsSQL and insertWinnersKeyedChunkIdentSQL record
// the identifiers this chunk applies, for the identifier-table restriction
// and the applied-row delete below.
const createWinnersKeyedChunkIdentsSQL = `
CREATE TEMP TABLE winners_keyed_chunk_idents (
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text
) ON COMMIT DROP
`

const insertWinnersKeyedChunkIdentSQL = `
INSERT INTO ` + winnersKeyedChunkIdentsTable + ` VALUES ($1, $2, $3, $4, $5)
`

// restrictWinnersKeyedIdentsToChunkSQL drops the identifiers this chunk does
// not apply, so the probe and the apply recompute (and delete stored winners)
// only inside the chunk. The dropped identifiers are not lost: their dirty
// rows were never deleted, or were converted to fact rows below.
var restrictWinnersKeyedIdentsToChunkSQL = `
DELETE FROM ` + winnersKeyedIdentsTable + ` AS ident
WHERE NOT ` + winnersKeyedChunkIdentMatch("ident") + `
`

// winnersKeyedChunkIdentMatch reports, for a row carrying the five identifier
// columns under alias, whether the chunk applies its identifier.
func winnersKeyedChunkIdentMatch(alias string) string {
	return `EXISTS (
    SELECT 1 FROM ` + winnersKeyedChunkIdentsTable + ` AS chunk
    WHERE (chunk.package_id, chunk.repository_id, chunk.subject_digest, chunk.c0, chunk.advisory_id)
        = (` + alias + `.package_id, ` + alias + `.repository_id, ` + alias + `.subject_digest, ` + alias + `.c0, ` + alias + `.advisory_id)
)`
}

// countWinnersKeyedActiveFactsSQL counts the kept facts the identifier probe
// built, before the operator facts are added: the per-tick kept-fact volume
// the chunk bound implies.
const countWinnersKeyedActiveFactsSQL = `
SELECT count(*) FROM ` + winnersKeyedActiveFactsTable

// convertWinnersKeyedScopeRemainderSQL turns the unapplied remainder of a
// partially applied scope row into fact dirty rows (same source), so the
// scope row can leave the log and the next tick resumes from named keys,
// not from re-expanding the whole generation and re-applying its prefix.
// A scope row with no applied identifier is left alone (nothing to resume from;
// it re-expands next tick), and a fully applied one converts nothing.
var convertWinnersKeyedScopeRemainderSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT DISTINCT 'fact', c.source, si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id
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
`

// deleteWinnersKeyedAppliedDirtySQL deletes exactly the admitted dirty rows
// this chunk applied, by seq, and tallies them by capture source: a fact row
// whose identifier was probed, and a scope row whose expansion has no
// unapplied identifier left — fully applied, or partially applied with its
// remainder converted above (after conversion every unapplied identifier is
// queued as a fact row, so the scope row's information survives its
// deletion). Scope rows with nothing applied and fact rows of unapplied
// identifiers stay queued.
var deleteWinnersKeyedAppliedDirtySQL = `
WITH deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_log AS d
    USING ` + winnersKeyedConsumedTable + ` AS c
    WHERE d.seq = c.seq
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
    RETURNING d.source
)
SELECT source, count(*) FROM deleted GROUP BY source
`

// deleteWinnersKeyedAllDirtySQL is the over-cap fallback's delete: the full
// rebuild recomputes every key, so every queued row is consumed, tallied by
// source. It matches the pre-chunking consume of the fallback path.
const deleteWinnersKeyedAllDirtySQL = `
WITH deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_log
    RETURNING source
)
SELECT source, count(*) FROM deleted GROUP BY source
`
