// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

// SQL of the keyed resweep and the audit of the #3389 canonical winners read
// model (#9805). The keyed path consumes the dirty log under the resweep's
// single-flight lock, builds a keyed active_facts through the keyed winners
// index (migration 291), and runs the shipped winner select body over it, so a
// tick costs the size of what changed instead of the size of the table. See
// (#9805).

// Names of the transaction-scoped temp tables a keyed tick and an audit build.
// Every one is created ON COMMIT DROP inside the transaction that uses it.
const (
	winnersKeyedConsumedTable    = "winners_keyed_consumed"
	winnersKeyedIdentsTable      = "winners_keyed_idents"
	winnersKeyedScopeIdentsTable = "winners_keyed_scope_idents"
	winnersKeyedChunkIdentsTable = "winners_keyed_chunk_idents"
	winnersKeyedActiveFactsTable = "winners_keyed_active_facts"
	winnersAuditCandidatesTable  = "winners_audit_candidates"
	winnersAuditStaleTable       = "winners_audit_stale"
	winnersAuditPendingTable     = "winners_audit_pending"
)

// createWinnersKeyedConsumedSQL and admitWinnersKeyedWindowSQL admit one
// bounded candidate window of the dirty log (#9805 chunked consume, #10827
// versioned): the oldest $1 rows by (captured_at, seq) are copied into a temp
// table under the resweep's single-flight lock, so no other tick can admit the
// same rows. The order is by capture, not by seq (#11075): the watermark
// horizon is min(captured_at) of the remainder, and the scope-remainder
// convert writes its debt at a new tail seq that keeps the old captured_at (or
// LEASTs it into a queued row that keeps its seq). FIFO by seq left that debt
// behind every newer row, so the horizon froze until the window reached it
// (about 34k rows on the QA environment). Admitting the oldest capture first
// lets every tick consume the row that holds the horizon down; seq is the
// unique tie-break. No index serves the order: at QA depth (56,698 rows) the
// planner runs a parallel seq scan and an in-memory top-N heapsort in about
// 21 ms, against about 4 ms for the seq primary-key scan (#11075 replica shim,
// docs/internal/evidence/11075-winners-admission-order.md), well under 1% of
// a keyed tick.
// Admission copies instead of deleting: which rows the chunk actually applies
// is known only after the identifier expansion and the identifier chunk bound,
// so the lock-then-delete drain (deleteWinnersKeyedAppliedDirtySQL, in the
// apply phase) removes exactly the applied rows by seq and every other row
// stays queued. The copy carries each row's dirty_version: the drain rechecks
// the locked log row against this admitted version, so a mark committed for
// an admitted key after admission bumps the log past the snapshot and stays
// queued for the next tick. A row committed after admission is not in the
// admission statement's snapshot, so it is never in the window whatever its
// captured_at, and stays for the next tick; a rollback changes nothing.
const createWinnersKeyedConsumedSQL = `
CREATE TEMP TABLE winners_keyed_consumed (
    seq bigint, kind text, source text, package_id text, repository_id text, subject_digest text,
    c0 text, advisory_id text, scope_id text, generation_id text, captured_at timestamptz, dirty_version bigint
) ON COMMIT DROP
`

const admitWinnersKeyedWindowSQL = `
INSERT INTO ` + winnersKeyedConsumedTable + `
SELECT seq, kind, source, package_id, repository_id, subject_digest, c0, advisory_id, scope_id, generation_id, captured_at, dirty_version
FROM supply_chain_impact_winners_dirty_log
ORDER BY captured_at, seq
LIMIT $1
`

// indexWinnersKeyedConsumedSeqSQL speeds the applied delete's dirty-log join
// (d.seq = c.seq): without it the delete probes the admitted window with a
// statistics-blind sequential scan per dirty-log row. Session-local to the
// tick transaction, like the table.
const indexWinnersKeyedConsumedSeqSQL = `
CREATE INDEX winners_keyed_consumed_seq_idx ON winners_keyed_consumed (seq)
`

// analyzeWinnersKeyedConsumedSQL gives the planner the real admitted-window
// count before the expansion and the delete plan on it.
const analyzeWinnersKeyedConsumedSQL = `ANALYZE ` + winnersKeyedConsumedTable

// countWinnersKeyedConsumedSQL counts the admitted window's rows.
const countWinnersKeyedConsumedSQL = `
SELECT count(*) FROM ` + winnersKeyedConsumedTable + `
`

// countWinnersKeyedRemainingSQL counts the dirty log's depth inside the tick
// transaction, after the applied rows were deleted: the watermark decision.
const countWinnersKeyedRemainingSQL = `
SELECT count(*) FROM supply_chain_impact_winners_dirty_log
`

// selectWinnersKeyedRemainingMinSQL reads the earliest capture time of the
// dirty log's residual set, inside the tick transaction after settle. It is
// NULL when the log is empty. The #10706 horizon is this min minus 1us, so
// every row remaining in the log at read time has captured_at above the
// stamped watermark. The documented exception is a row whose INSERT executed
// before the tick clock was read but committed after the tick's snapshot: it
// stays covered for exactly one tick and the next tick applies it (pinned by
// HorizonDrainedLateCommitLive and HorizonIdleLateCommitLive).
const selectWinnersKeyedRemainingMinSQL = `
SELECT min(captured_at) FROM supply_chain_impact_winners_dirty_log
`

// selectWinnersKeyedTickClockSQL reads the DB clock inside the tick
// transaction. The tick reads it once, before admission, and every branch
// that stamps S stamps that one read (#10706): the watermark, captured_at,
// and the intent bound the advisory handler waits on all live in the DB
// clock domain. A row that both executes and commits after that read is
// never covered by the stamp; a row that executed before it but commits
// after the tick's snapshot stays covered for exactly one tick and the next
// tick applies it (see the late-commit tests).
const selectWinnersKeyedTickClockSQL = `
SELECT clock_timestamp()
`

// selectWinnersWatermarkSQL reads the committed watermark: the exact value
// the tick stamped, after GREATEST resolved it against the previous one.
const selectWinnersWatermarkSQL = `
SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1
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

// winnersIdentExprsAs renders the same five identifier columns with the
// identifier-table column names, for a CREATE TABLE AS SELECT: five bare
// COALESCE expressions would all take the column name "coalesce" and fail
// with SQLSTATE 42701 (duplicate column name).
func winnersIdentExprsAs(prefix string) string {
	payload := prefix + ".payload"
	return `COALESCE(` + payload + `->>'package_id', '') AS package_id,
       COALESCE(` + payload + `->>'repository_id', '') AS repository_id,
       COALESCE(` + payload + `->>'subject_digest', '') AS subject_digest,
       COALESCE(NULLIF(` + payload + `->>'cve_id', ''), NULLIF(` + payload + `->>'advisory_id', ''), '') AS c0,
       COALESCE(` + payload + `->>'advisory_id', '') AS advisory_id`
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

// buildWinnersKeyedScopeIdentsSQL expands the admitted scope rows to the
// identifiers of their generations, keeping the (scope, generation) each
// identifier came from. Chunked consume needs the provenance: a scope row
// only leaves the log once every identifier of its expansion was applied, or
// once its unapplied remainder was converted to fact rows.
var buildWinnersKeyedScopeIdentsSQL = `
CREATE TEMP TABLE winners_keyed_scope_idents ON COMMIT DROP AS
SELECT pending.scope_id, pending.generation_id, ` + winnersIdentExprsAs("fact") + `
FROM (SELECT DISTINCT scope_id, generation_id FROM ` + winnersKeyedConsumedTable + ` WHERE kind = 'scope') AS pending
JOIN fact_records AS fact
  ON fact.scope_id = pending.scope_id
 AND fact.generation_id = pending.generation_id
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact.is_tombstone = FALSE`

// indexWinnersKeyedScopeIdentsSQL speeds the identifier build and the settle
// joins on (scope_id, generation_id): without it every consumer of the
// scope expansion probes those 42k-class rows with a statistics-blind
// sequential scan. Session-local to the tick transaction, like the table.
const indexWinnersKeyedScopeIdentsSQL = `
CREATE INDEX winners_keyed_scope_idents_scope_idx ON winners_keyed_scope_idents (scope_id, generation_id)
`

// analyzeWinnersKeyedScopeIdentsSQL gives the planner the real expansion size
// before the identifier table and every settle statement plan on it.
const analyzeWinnersKeyedScopeIdentsSQL = `ANALYZE ` + winnersKeyedScopeIdentsTable

// buildWinnersKeyedIdentsSQL builds the distinct identifier set of a keyed tick
// with the age of the oldest consumed row behind each identifier (#10706
// oldest-first chunk): fact rows directly, scope-expanded identifiers through
// the scope join, which recovers the per-row age the scope table's DISTINCT
// dropped. The chunk orders by that age. The audit seeds the identifier table
// itself and never runs this statement.
var buildWinnersKeyedIdentsSQL = `
CREATE TEMP TABLE winners_keyed_idents ON COMMIT DROP AS
SELECT package_id, repository_id, subject_digest, c0, advisory_id, MIN(captured_at) AS age_captured_at, MIN(seq) AS age_seq
FROM (
    SELECT package_id, repository_id, subject_digest, c0, advisory_id, captured_at, seq FROM ` + winnersKeyedConsumedTable + ` WHERE kind = 'fact'
    UNION ALL
    SELECT si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id, c.captured_at, c.seq
    FROM ` + winnersKeyedConsumedTable + ` AS c JOIN ` + winnersKeyedScopeIdentsTable + ` AS si ON si.scope_id = c.scope_id AND si.generation_id = c.generation_id
    WHERE c.kind = 'scope'
) AS aged
GROUP BY package_id, repository_id, subject_digest, c0, advisory_id`

// analyzeWinnersKeyedIdentsSQL gives the planner the real identifier count, so
// it probes the keyed winners index once per identifier instead of choosing a
// scan from a default estimate.
const analyzeWinnersKeyedIdentsSQL = `ANALYZE ` + winnersKeyedIdentsTable

// winnersKeyedBuildIdentsStmts is the ordered build_idents phase: the scope
// expansion is created, indexed, and analyzed before the identifier table
// reads it, so the identifier join and every later consumer plan on real
// statistics. runKeyedOrFull and the explain export both run this list, so
// the explained plans are the plans the tick gets.
var winnersKeyedBuildIdentsStmts = []string{
	buildWinnersKeyedScopeIdentsSQL,
	indexWinnersKeyedScopeIdentsSQL,
	analyzeWinnersKeyedScopeIdentsSQL,
	buildWinnersKeyedIdentsSQL,
	analyzeWinnersKeyedIdentsSQL,
}

// SQL of the chunked consume (#9805). The arbiter ruling behind this lane is
// chunk-build-sufficient: the keyed apply scales per changed/deleted row in
// the identifier set (no full-table rewrite in keyed apply), so bounding the
// post-expansion identifier set per tick bounds the apply, with no separate
// apply bound. The bound therefore applies to post-expansion identifiers,
// never to raw dirty rows: the admitted dirty-row window below only limits
// the candidates the expansion reads, and the truncation decision is made on
// identifiers.
//
// The chunk is the oldest-first prefix of the post-expansion identifier
// set (#10706). The active_facts build is the shipped per-identifier probe
// over the 291
// five-tuple keyed index (buildWinnersKeyedActiveFactsSQL, unchanged): with
// all five equalities as index conditions the heap fetches only the facts a
// dirty key keeps, so no candidate-row budget is needed. The lane this file
// replaced probed per package triple over the triple-only package-lookup
// index; migration 293 retired that index, so per-triple probing has no
// access path anymore.

// buildWinnersKeyedActiveFactsSQL builds the keyed active_facts: the active
// findings of exactly the identifiers in the identifier table.
//
// The probe runs once per identifier against the keyed winners index
// (fact_records_supply_chain_impact_winners_keyed_idx, migration 291), whose
// key is the package triple plus the coalesced c0 and advisory expressions
// below, so all five equalities are index conditions and the heap fetches only
// the facts a dirty key keeps (about 13 kept facts per identifier on QA). The
// identifier table holds distinct five-tuples (it is built with GROUP BY over UNION ALL), and a
// fact carries exactly one c0 and advisory_id, so no fact can match two
// identifiers and the probe cannot duplicate a row. The previous per-triple
// probe used the package-lookup index, keyed on the triple only: it fetched
// every candidate of a touched triple as wide fact.* rows and discarded most
// after the heap fetch (measured on QA, #9975: 15,888 fetched rows carrying
// 160 MB of payload for 2,720 kept on a 200-identifier sparse tick, and
// build_active_facts at 68% of tick time with p95 17.1s). Moving the c0 and
// advisory comparisons inside the lateral without the index cut that probe by
// only about 37% (the heap still fetched every candidate), which is why the
// expressions are in the index. Migration 293 (#10544) drops that
// package-lookup index: at rows=1 the planner costed the two indexes
// within 0.01 on a correlated triple and kept choosing the triple-only
// one on QA, so the keyed index above is now the only triple-leading
// index. This statement is unchanged. The probe sits in a MATERIALIZED CTE so a join
// order picked from default statistics on ingestion_scopes cannot put it
// inside a scope loop; a measured run with that plan took 28 million buffer
// hits for a 2,000-row result. OFFSET 0 keeps the lateral subquery from being
// flattened into a scan. The writer serializes package_id, repository_id and
// subject_digest on every finding, so the equality probe cannot miss a row on
// a NULL key.
//
// The remaining floor is one index descent plus the kept facts' heap rows per
// identifier, held in the CTE's tuplestore.
const buildWinnersKeyedActiveFactsSQL = `
CREATE TEMP TABLE winners_keyed_active_facts ON COMMIT DROP AS
WITH ident_facts AS MATERIALIZED (
    SELECT fact.*
    FROM ` + winnersKeyedIdentsTable + ` AS ident
    CROSS JOIN LATERAL (
        SELECT f.*
        FROM fact_records AS f
        WHERE (f.payload->>'package_id') = ident.package_id
          AND (f.payload->>'repository_id') = ident.repository_id
          AND (f.payload->>'subject_digest') = ident.subject_digest
          AND COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '') = ident.c0
          AND COALESCE(f.payload->>'advisory_id', '') = ident.advisory_id
          AND f.fact_kind = 'reducer_supply_chain_impact_finding'
          AND f.is_tombstone = FALSE
        OFFSET 0
    ) AS fact
)
SELECT fact.*
FROM ident_facts AS fact
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

// countWinnersKeyedActiveFactsSQL counts the kept facts the identifier probe
// built, before the operator facts are added: the per-tick kept-fact volume
// the chunk bound implies.
const countWinnersKeyedActiveFactsSQL = `
SELECT count(*) FROM ` + winnersKeyedActiveFactsTable

// countWinnersKeyedScannedCopiesSQL counts the pre-filter copies the
// identifier probe scans: every finding the five-tuple probe matches, before
// the active-generation join discards the retired and superseded ones
// (#11308). It mirrors the ident_facts probe of
// buildWinnersKeyedActiveFactsSQL without the scope join, and stays narrow
// (SELECT 1) so it avoids materializing wide rows: it is index-only-scan
// eligible and detoasts only on heap visits. The build already paid the
// TOAST reads, and this count only revisits the index and heap tuples. It runs
// after the build in the same transaction; a fact committed between the two
// statements shifts the count by those rows, a same-tick approximation that
// washes out over ticks. It is deliberately not part of the contract pin:
// it reads nothing the resweep writes.
const countWinnersKeyedScannedCopiesSQL = `
SELECT count(*) FROM ` + winnersKeyedIdentsTable + ` AS ident
CROSS JOIN LATERAL (
    SELECT 1
    FROM fact_records AS f
    WHERE (f.payload->>'package_id') = ident.package_id
      AND (f.payload->>'repository_id') = ident.repository_id
      AND (f.payload->>'subject_digest') = ident.subject_digest
      AND COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '') = ident.c0
      AND COALESCE(f.payload->>'advisory_id', '') = ident.advisory_id
      AND f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE
    OFFSET 0
) AS fact
`

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
// shape and the same row comparison, restricted to the identifier set. $1 is
// the stamp candidate: the DB-clock tick stamp S when the log drained, or
// the horizon H = min(captured_at) of the remainder minus 1us when rows are
// still queued (#10706). The stamp is GREATEST(previous, candidate), so the
// watermark never regresses. It returns inserted, updated, deleted,
// unchanged, the keys recomputed (winners now produced plus winners deleted),
// and the previous watermark.
var applyWinnersKeyedSQL = applyWinnersKeyedBodySQL + `
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = GREATEST(supply_chain_impact_winners_materialization.materialized_at, EXCLUDED.materialized_at)
RETURNING
    (SELECT count(*) FROM changed WHERE is_new),
    (SELECT count(*) FROM changed WHERE NOT is_new),
    (SELECT count(*) FROM deleted),
    (SELECT count(*) FROM winners_now) - (SELECT count(*) FROM changed),
    (SELECT count(*) FROM winners_now) + (SELECT count(*) FROM deleted),
    (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
`

var applyWinnersKeyedBodySQL = `
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
`

// stampWinnersWatermarkSQL stamps the watermark candidate for a tick that
// consumed nothing: $1 is the DB-clock tick stamp S when the log is empty, or
// the horizon H = min(captured_at) minus 1us when rows landed after admission
// (#10706). A watermark that only moves when something changed would make an
// idle table read as stale. The stamp is GREATEST(previous, candidate), so the
// watermark never regresses; it returns the one it replaced.
// #nosec G101 -- SQL statement text, not a credential.
const stampWinnersWatermarkSQL = `
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = GREATEST(supply_chain_impact_winners_materialization.materialized_at, EXCLUDED.materialized_at)
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

// selectWinnersDirtyLogStatsSQL reads the dirty log's depth and oldest
// capture time beside the staging queue's residual columns ($1 is the
// staging exact-count cap) and the database clock, in one snapshot
// (#10837 direction C, #11129). The maintainer samples before the tick's
// collapse, so the gauges must see the staged rows too; the staging part is
// the residual read's bounded walk, never a whole-queue count.
var selectWinnersDirtyLogStatsSQL = winnersStagingHeadCTE + `
SELECT l.c, l.m, ` + winnersStagingResidualColumns + `,
       clock_timestamp()
FROM (SELECT count(*) AS c, min(captured_at) AS m FROM supply_chain_impact_winners_dirty_log) AS l
`

// captureWinnersDirtyFactsSQL stages the writer's changed identifiers, in
// the writer's own transaction (#10837 direction C). One row per mark: a
// re-mark of a queued key appends another staged row instead of waiting on
// the log's conflict arbiter, and the maintainer tick's collapse restores
// the version math (dirty_version += staged marks, first captured_at and
// first-marker source win). The Go sort in CaptureWinnersDirtyFacts still
// deduplicates one statement's identifiers, so one statement never stages
// one unit twice; the SELECT needs no ORDER BY (no locks to take in order)
// and no ON CONFLICT (no unique index on the hot path).
const captureWinnersDirtyFactsSQL = `
INSERT INTO supply_chain_impact_winners_dirty_staging
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', package_id, repository_id, subject_digest, c0, advisory_id
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
     AS ident(package_id, repository_id, subject_digest, c0, advisory_id)
`
