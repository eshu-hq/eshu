// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// SQL of the chunked consume (#9805 CODE-NEEDED-1). The shim verdict was
// candidate volume times cold I/O: one dirty key makes the probe fetch its
// whole (package_id, repository_id, subject_digest) triple, so bounding keys
// or dirty rows alone cannot bound a transaction. These statements bound the
// transaction at candidate admission instead, in both truncation orders: the
// admitted window (admitWinnersKeyedWindowSQL), the distinct triples probed
// (selectWinnersKeyedChunkTriplesSQL), the candidate rows fetched per triple
// (insertWinnersKeyedTripleFactsSQL reports them, the Go loop stops at the
// budget), and the apply, whose identifier table is first restricted to the
// probed triples (restrictWinnersKeyedIdentsToChunkSQL).

// selectWinnersKeyedChunkTriplesSQL lists the identifier set's distinct
// triples in a fixed order, $1 of them. The caller asks for one past the
// bound, so a full page proves the set continues past this chunk.
const selectWinnersKeyedChunkTriplesSQL = `
SELECT package_id, repository_id, subject_digest
FROM (SELECT DISTINCT package_id, repository_id, subject_digest FROM ` + winnersKeyedIdentsTable + `) AS triple
ORDER BY package_id, repository_id, subject_digest
LIMIT $1
`

// createWinnersKeyedChunkTriplesSQL and insertWinnersKeyedChunkTripleSQL
// record the triples this chunk probed, for the identifier restriction and
// the applied-row delete below.
const createWinnersKeyedChunkTriplesSQL = `
CREATE TEMP TABLE winners_keyed_chunk_triples (
    package_id text, repository_id text, subject_digest text
) ON COMMIT DROP
`

const insertWinnersKeyedChunkTripleSQL = `
INSERT INTO ` + winnersKeyedChunkTriplesTable + ` VALUES ($1, $2, $3)
`

// createWinnersKeyedActiveFactsEmptySQL creates the chunked active_facts with
// fact_records' shape. The per-triple inserts below fill it; the shipped
// winner select body then runs over it unchanged.
const createWinnersKeyedActiveFactsEmptySQL = `
CREATE TEMP TABLE winners_keyed_active_facts ON COMMIT DROP AS
SELECT * FROM fact_records WHERE FALSE
`

// insertWinnersKeyedTripleFactsSQL probes one triple ($1..$3) exactly as
// buildWinnersKeyedActiveFactsSQL probes each of its triples — the same
// MATERIALIZED lateral over the package-lookup index, OFFSET 0 against
// flattening, the same identifier and active-generation joins — inserts the
// facts that name a dirty key of the triple, and returns the candidate rows
// the probe fetched and the facts it kept. The fetched count is the heap
// work the row budget bounds; it is read from the materialized probe, so it
// costs no second fetch.
const insertWinnersKeyedTripleFactsSQL = `
WITH fetched AS MATERIALIZED (
    SELECT fact.*
    FROM (SELECT $1::text AS package_id, $2::text AS repository_id, $3::text AS subject_digest) AS triple
    CROSS JOIN LATERAL (
        SELECT f.*
        FROM fact_records AS f
        WHERE (f.payload->>'package_id') = triple.package_id
          AND (f.payload->>'repository_id') = triple.repository_id
          AND (f.payload->>'subject_digest') = triple.subject_digest
          AND f.fact_kind = 'reducer_supply_chain_impact_finding'
          AND f.is_tombstone = FALSE
        OFFSET 0
    ) AS fact
),
kept AS (
    INSERT INTO ` + winnersKeyedActiveFactsTable + `
    SELECT fact.*
    FROM fetched AS fact
    JOIN ` + winnersKeyedIdentsTable + ` AS ident
      ON ident.package_id = (fact.payload->>'package_id')
     AND ident.repository_id = (fact.payload->>'repository_id')
     AND ident.subject_digest = (fact.payload->>'subject_digest')
     AND ident.c0 = COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), '')
     AND ident.advisory_id = COALESCE(fact.payload->>'advisory_id', '')
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE generation.status = 'active'
    RETURNING 1
)
SELECT (SELECT count(*) FROM fetched), (SELECT count(*) FROM kept)
`

// restrictWinnersKeyedIdentsToChunkSQL drops the identifiers of triples this
// chunk did not probe, so the apply recomputes (and deletes stored winners)
// only inside the chunk. The dropped identifiers are not lost: their dirty
// rows were never deleted, or were converted to fact rows below.
const restrictWinnersKeyedIdentsToChunkSQL = `
DELETE FROM ` + winnersKeyedIdentsTable + ` AS ident
WHERE NOT EXISTS (
    SELECT 1 FROM ` + winnersKeyedChunkTriplesTable + ` AS chunk
    WHERE chunk.package_id = ident.package_id
      AND chunk.repository_id = ident.repository_id
      AND chunk.subject_digest = ident.subject_digest
)
`

// winnersKeyedChunkTripleMatch reports, for a row carrying the three triple
// columns under alias, whether the chunk probed its triple.
func winnersKeyedChunkTripleMatch(alias string) string {
	return `EXISTS (
    SELECT 1 FROM ` + winnersKeyedChunkTriplesTable + ` AS chunk
    WHERE chunk.package_id = ` + alias + `.package_id
      AND chunk.repository_id = ` + alias + `.repository_id
      AND chunk.subject_digest = ` + alias + `.subject_digest
)`
}

// convertWinnersKeyedScopeRemainderSQL turns the unapplied remainder of a
// partially applied scope row into fact dirty rows (same source), so the
// scope row can leave the log and the next tick resumes from named keys,
// not from re-expanding the whole generation and re-applying its prefix.
// A scope row with no applied triple is left alone (nothing to resume from;
// it re-expands next tick), and a fully applied one converts nothing.
var convertWinnersKeyedScopeRemainderSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT DISTINCT 'fact', c.source, si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id
FROM ` + winnersKeyedConsumedTable + ` AS c
JOIN ` + winnersKeyedScopeIdentsTable + ` AS si
  ON si.scope_id = c.scope_id AND si.generation_id = c.generation_id
WHERE c.kind = 'scope'
  AND NOT ` + winnersKeyedChunkTripleMatch("si") + `
  AND EXISTS (
      SELECT 1 FROM ` + winnersKeyedScopeIdentsTable + ` AS applied
      WHERE applied.scope_id = c.scope_id AND applied.generation_id = c.generation_id
        AND ` + winnersKeyedChunkTripleMatch("applied") + `
  )
`

// deleteWinnersKeyedAppliedDirtySQL deletes exactly the admitted dirty rows
// this chunk applied, by seq, and tallies them by capture source: a fact row
// whose triple was probed, and a scope row whose expansion has no unprobed
// triple left — fully applied, or partially applied with its remainder
// converted above (after conversion every unprobed identifier is queued as a
// fact row, so the scope row's information survives its deletion). Scope
// rows with nothing applied and fact rows of unprobed triples stay queued.
var deleteWinnersKeyedAppliedDirtySQL = `
WITH deleted AS (
    DELETE FROM supply_chain_impact_winners_dirty_log AS d
    USING ` + winnersKeyedConsumedTable + ` AS c
    WHERE d.seq = c.seq
      AND (
          (c.kind = 'fact' AND ` + winnersKeyedChunkTripleMatch("c") + `)
          OR (c.kind = 'scope' AND (
              NOT EXISTS (
                  SELECT 1 FROM ` + winnersKeyedScopeIdentsTable + ` AS si
                  WHERE si.scope_id = c.scope_id AND si.generation_id = c.generation_id
                    AND NOT ` + winnersKeyedChunkTripleMatch("si") + `
              )
              OR EXISTS (
                  SELECT 1 FROM ` + winnersKeyedScopeIdentsTable + ` AS applied
                  WHERE applied.scope_id = c.scope_id AND applied.generation_id = c.generation_id
                    AND ` + winnersKeyedChunkTripleMatch("applied") + `
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
