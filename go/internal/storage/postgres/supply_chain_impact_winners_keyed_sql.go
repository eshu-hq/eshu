// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

// SQL of the keyed resweep and the audit of the #3389 canonical winners read
// model (#9805). The keyed path consumes the dirty log under the resweep's
// single-flight lock, builds a keyed active_facts from the existing
// package-lookup index, and runs the shipped winner select body over it, so a
// tick costs the size of what changed instead of the size of the table. See
// docs/internal/evidence/9805-winners-keyed-resweep.md.

// Names of the transaction-scoped temp tables a keyed tick and an audit build.
// Every one is created ON COMMIT DROP inside the transaction that uses it.
const (
	winnersKeyedConsumedTable    = "winners_keyed_consumed"
	winnersKeyedIdentsTable      = "winners_keyed_idents"
	winnersKeyedActiveFactsTable = "winners_keyed_active_facts"
	winnersAuditCandidatesTable  = "winners_audit_candidates"
	winnersAuditStaleTable       = "winners_audit_stale"
	winnersAuditPendingTable     = "winners_audit_pending"
)

// createWinnersKeyedConsumedSQL and consumeWinnersDirtyLogSQL consume the dirty
// log: DELETE ... RETURNING moves exactly the rows visible to this statement
// into a temp table, so a row committed after the statement's snapshot stays in
// the log for the next tick, and a rollback returns every consumed row. The
// DELETE takes no lock a writer's INSERT waits on.
const createWinnersKeyedConsumedSQL = `
CREATE TEMP TABLE winners_keyed_consumed (
    kind text, source text, package_id text, repository_id text, subject_digest text,
    c0 text, advisory_id text, scope_id text, generation_id text
) ON COMMIT DROP
`

const consumeWinnersDirtyLogSQL = `
WITH consumed AS (
    DELETE FROM supply_chain_impact_winners_dirty_log
    RETURNING kind, source, package_id, repository_id, subject_digest, c0, advisory_id, scope_id, generation_id
)
INSERT INTO ` + winnersKeyedConsumedTable + `
SELECT kind, source, package_id, repository_id, subject_digest, c0, advisory_id, scope_id, generation_id
FROM consumed
`

// countWinnersKeyedConsumedSQL counts the consumed rows by capture source.
const countWinnersKeyedConsumedSQL = `
SELECT source, count(*) FROM ` + winnersKeyedConsumedTable + ` GROUP BY source
`

// winnersIdentExprs renders the five identifier columns of a fact row, with the
// same empty-string and first-key rules as the winner select.
func winnersIdentExprs(prefix string) string {
	payload := prefix + ".payload"
	return `COALESCE(` + payload + `->>'package_id', ''),
       COALESCE(` + payload + `->>'repository_id', ''),
       COALESCE(` + payload + `->>'subject_digest', ''),
       COALESCE(NULLIF(` + payload + `->>'cve_id', ''), NULLIF(` + payload + `->>'advisory_id', ''), ''),
       COALESCE(` + payload + `->>'advisory_id', '')`
}

// winnersScopeExpansionSelect expands the scope rows of table into the
// identifiers of the findings those (scope, generation) pairs hold, through the
// active-scan index. A scope row names a generation whose membership in the
// active set changed, so every finding of it can have moved a winner.
func winnersScopeExpansionSelect(table string) string {
	return `SELECT ` + winnersIdentExprs("fact") + `
FROM (SELECT DISTINCT scope_id, generation_id FROM ` + table + ` WHERE kind = 'scope') AS pending
JOIN fact_records AS fact
  ON fact.scope_id = pending.scope_id
 AND fact.generation_id = pending.generation_id
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact.is_tombstone = FALSE`
}

// buildWinnersKeyedIdentsSQL builds the distinct identifier set of a keyed tick
// from its consumed fact rows and the expansion of its consumed scope rows.
var buildWinnersKeyedIdentsSQL = `
CREATE TEMP TABLE winners_keyed_idents ON COMMIT DROP AS
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM ` + winnersKeyedConsumedTable + `
WHERE kind = 'fact'
UNION
` + winnersScopeExpansionSelect(winnersKeyedConsumedTable)

// analyzeWinnersKeyedIdentsSQL gives the planner the real identifier count, so
// it probes the package-lookup index once per identifier instead of choosing a
// scan from a default estimate.
const analyzeWinnersKeyedIdentsSQL = `ANALYZE ` + winnersKeyedIdentsTable

// buildWinnersKeyedActiveFactsSQL builds the keyed active_facts: the active
// findings of exactly the identifiers in the identifier table.
//
// The probe runs once per DISTINCT (package_id, repository_id, subject_digest)
// triple, not once per identifier, because the package-lookup index
// (fact_records_supply_chain_impact_package_lookup_idx) is keyed on those three
// payload columns only: every identifier of a triple fetches the whole triple,
// and the c0 and advisory comparisons can only filter after the heap fetch. On a
// hot triple of about 5,000 candidates, fifty dirty keys fetched and discarded
// about 250,000 rows. The probe sits in a MATERIALIZED CTE so the planner cannot
// join the triples back to the identifiers and run the lateral once per
// identifier again, and so a join order picked from default statistics on
// ingestion_scopes cannot put the probe inside a scope loop. A measured run
// with that plan took 28 million buffer hits for a 2,000-row result. OFFSET 0 keeps the
// lateral subquery from being flattened into a scan. The identifier join then
// keeps the facts whose c0 and advisory_id name a dirty key. The writer
// serializes package_id, repository_id and subject_digest on every finding, so
// the equality probe cannot miss a row on a NULL key.
//
// The remaining floor is one fetch of each touched triple's candidates, held in
// the CTE's tuplestore (it spills to a temp file past work_mem): about 5,000
// rows for one dirty key on a hot triple.
const buildWinnersKeyedActiveFactsSQL = `
CREATE TEMP TABLE winners_keyed_active_facts ON COMMIT DROP AS
WITH triple_facts AS MATERIALIZED (
    SELECT fact.*
    FROM (SELECT DISTINCT package_id, repository_id, subject_digest FROM ` + winnersKeyedIdentsTable + `) AS triple
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
)
SELECT fact.*
FROM triple_facts AS fact
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
`

// addWinnersKeyedOperatorFactsSQL adds every active operator-suppression fact to
// the keyed active_facts. An operator payload may lack package_id or
// subject_digest, so the identifier probe cannot find it, yet its override
// must reach every source key it matches; the operator set is small and bounded
// by the operators' suppressions, not by the findings.
const addWinnersKeyedOperatorFactsSQL = `
INSERT INTO ` + winnersKeyedActiveFactsTable + `
SELECT fact.*
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.scope_id = 'operator:vulnerability_suppressions'
  AND fact.fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND NOT EXISTS (
      SELECT 1 FROM ` + winnersKeyedActiveFactsTable + ` AS existing WHERE existing.fact_id = fact.fact_id
  )
`

const analyzeWinnersKeyedActiveFactsSQL = `ANALYZE ` + winnersKeyedActiveFactsTable

// winnersKeyedSelectSQL is the shipped winner select body over the keyed
// active_facts temp table. The body is byte-identical to the full path's.
var winnersKeyedSelectSQL = `WITH active_facts AS (SELECT * FROM ` + winnersKeyedActiveFactsTable + `)` +
	supplyChainImpactWinnerBodySQL

// winnersStoredIdentMatch matches stored winner rows (alias w) to the
// identifier table; the winners table keeps the same coalesced columns.
const winnersStoredIdentMatch = `(w.package_id, w.repository_id, w.subject_digest,
        COALESCE(NULLIF(w.cve_id, ''), NULLIF(w.advisory_id, ''), ''), w.advisory_id)
       IN (SELECT package_id, repository_id, subject_digest, c0, advisory_id FROM ` + winnersKeyedIdentsTable + `)`

// applyWinnersKeyedSQL recomputes the identifier set's winners and writes only
// the rows that are new or changed, deletes stored winners of the identifier
// set that are no longer produced, and stamps the watermark. It is the keyed
// twin of rebuildSupplyChainImpactWinnersSQL: the same changed/upserted/deleted
// shape and the same row comparison, restricted to the identifier set. $1 is the
// new watermark. It returns inserted, updated, deleted, unchanged, the keys
// recomputed (winners now produced plus winners deleted), and the previous
// watermark.
var applyWinnersKeyedSQL = `
WITH winners_now AS (` + winnersKeyedSelectSQL + `),
changed AS MATERIALIZED (
    SELECT n.*, (w.canonical_key IS NULL) AS is_new
    FROM winners_now AS n
    LEFT JOIN supply_chain_impact_canonical_winners AS w
      ON w.canonical_key = n.canonical_key
    WHERE w.canonical_key IS NULL
       OR ` + supplyChainImpactWinnerRowExpr("n") + ` IS DISTINCT FROM ` + supplyChainImpactWinnerRowExpr("w") + `
),
upserted AS (
    INSERT INTO supply_chain_impact_canonical_winners (
        canonical_key, ` + strings.Join(supplyChainImpactWinnerDataColumns, ", ") + `, materialized_at
    )
    SELECT
        canonical_key, ` + strings.Join(supplyChainImpactWinnerDataColumns, ", ") + `, $1
    FROM changed
    ON CONFLICT (canonical_key) DO UPDATE SET
        ` + supplyChainImpactWinnerConflictSet() + `
),
deleted AS (
    DELETE FROM supply_chain_impact_canonical_winners AS w
    WHERE ` + winnersStoredIdentMatch + `
      AND NOT EXISTS (SELECT 1 FROM winners_now AS n WHERE n.canonical_key = w.canonical_key)
    RETURNING 1
)
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at
RETURNING
    (SELECT count(*) FROM changed WHERE is_new),
    (SELECT count(*) FROM changed WHERE NOT is_new),
    (SELECT count(*) FROM deleted),
    (SELECT count(*) FROM winners_now) - (SELECT count(*) FROM changed),
    (SELECT count(*) FROM winners_now) + (SELECT count(*) FROM deleted),
    (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
`

// stampWinnersWatermarkSQL stamps the watermark for a tick that consumed
// nothing: the table is current as of now, and a watermark that only moves when
// something changed would make an idle table read as stale. $1 is the new
// watermark; it returns the one it replaced.
// #nosec G101 -- SQL statement text, not a credential.
const stampWinnersWatermarkSQL = `
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at
RETURNING (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
`

// winnersKeyedStoredEstimateSQL estimates the stored winner count from the
// planner statistics (reltuples is -1 until the table is first analyzed) and
// falls back to an exact count then, so a young table still gets the fraction
// cap. Autovacuum keeps reltuples current on a populated table.
const winnersKeyedStoredEstimateSQL = `
SELECT CASE WHEN c.reltuples >= 0 THEN c.reltuples::bigint
            ELSE (SELECT count(*) FROM supply_chain_impact_canonical_winners) END
FROM pg_class AS c
WHERE c.oid = 'supply_chain_impact_canonical_winners'::regclass
`

// countWinnersKeyedIdentsSQL counts the identifier table.
const countWinnersKeyedIdentsSQL = `SELECT count(*) FROM ` + winnersKeyedIdentsTable

// selectWinnersDirtyLogStatsSQL reads the dirty log's depth and the age of its
// oldest row.
const selectWinnersDirtyLogStatsSQL = `
SELECT count(*), COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(captured_at)), 0)::float8
FROM supply_chain_impact_winners_dirty_log
`

// captureWinnersDirtyFactsSQL appends the writer's changed identifiers to the
// dirty log, in the writer's own transaction.
const captureWinnersDirtyFactsSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', package_id, repository_id, subject_digest, c0, advisory_id
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
     AS ident(package_id, repository_id, subject_digest, c0, advisory_id)
`
