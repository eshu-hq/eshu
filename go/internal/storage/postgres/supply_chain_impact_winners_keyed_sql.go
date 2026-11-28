// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

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
// #11075 (retired evidence record)), well under 1% of
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

// winnersIdentExprs renders the five identifier columns over the
// once-extracted record (#11120); the caller joins winnersDetoastFactLateral.
func winnersIdentExprs() string {
	return `COALESCE(d.package_id, ''),
       COALESCE(d.repository_id, ''),
       COALESCE(d.subject_digest, ''),
       COALESCE(NULLIF(d.cve_id, ''), NULLIF(d.advisory_id, ''), ''),
       COALESCE(d.advisory_id, '')`
}

// winnersIdentExprsAs renders the same five identifier columns with the
// identifier-table column names, for a CREATE TABLE AS SELECT: five bare
// COALESCE expressions would all take the column name "coalesce" and fail
// with SQLSTATE 42701 (duplicate column name).
func winnersIdentExprsAs() string {
	return `COALESCE(d.package_id, '') AS package_id,
       COALESCE(d.repository_id, '') AS repository_id,
       COALESCE(d.subject_digest, '') AS subject_digest,
       COALESCE(NULLIF(d.cve_id, ''), NULLIF(d.advisory_id, ''), '') AS c0,
       COALESCE(d.advisory_id, '') AS advisory_id`
}

// winnersScopeExpansionSelect expands the scope rows of table into the
// identifiers of the findings those (scope, generation) pairs hold, through the
// active-scan index. A scope row names a generation whose membership in the
// active set changed, so every finding of it can have moved a winner.
func winnersScopeExpansionSelect(table string) string {
	return `SELECT ` + winnersIdentExprs() + `
FROM (SELECT DISTINCT scope_id, generation_id FROM ` + table + ` WHERE kind = 'scope') AS pending
JOIN fact_records AS fact
  ON fact.scope_id = pending.scope_id
 AND fact.generation_id = pending.generation_id` + winnersDetoastFactLateral + `
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
SELECT pending.scope_id, pending.generation_id, ` + winnersIdentExprsAs() + `
FROM (SELECT DISTINCT scope_id, generation_id FROM ` + winnersKeyedConsumedTable + ` WHERE kind = 'scope') AS pending
JOIN fact_records AS fact
  ON fact.scope_id = pending.scope_id
 AND fact.generation_id = pending.generation_id` + winnersDetoastFactLateral + `
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
	buildWinnersKeyedScopeIdentsFromSupportsSQL,
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
// The chunk is the oldest-first budgeted prefix of the post-expansion
// identifier set (#10706, Dec 6(ii)): the identifier bound caps how many
// identifiers a tick applies, and the scanned-copies budget caps how many
// pre-filter copies the probe scans to apply them. The active_facts build
// probes per identifier over the 291 five-tuple keyed index (all five
// equalities are index conditions), and the once-per-tick live_gens set
// keeps only live-generation copies (#10839). Per-triple probing has no
// access path anymore: migration 293 retired that index.

// buildWinnersKeyedActiveFactsSQL builds the keyed active_facts: the active
// findings of exactly the identifiers in the identifier table.
//
// The probe runs once per identifier against the keyed winners index
// (fact_records_supply_chain_impact_winners_keyed_idx, migration 291), whose
// key is the package triple plus the coalesced c0 and advisory expressions
// below, so all five equalities are index conditions. The identifier table
// holds distinct five-tuples (it is built with GROUP BY over UNION ALL), and a
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
// index.
//
// Liveness is checked once per distinct live generation (#10839), not once
// per candidate copy: one identifier fans out to hundreds of active copies
// (QA replica 2026-11-24: 50,123 copies over 200 identifiers collapse to
// 236 live pairs), and the shipped per-row liveness join cost ~10x the fact
// fetch plus a 73 MB tuplestore spill. The narrow lateral below selects only
// (fact_id, scope_id, generation_id) per copy, live_gens resolves each pair
// once, and the final fetch reads wide rows only for live copies by
// fact_id. Row-set equivalence holds by construction and is proven by QA
// differential (see #10839 (retired evidence record)).
// The probe sits in MATERIALIZED CTEs so a join order picked from default
// statistics on ingestion_scopes cannot put it inside a scope loop (a
// measured run with that plan took 28 million buffer hits for a 2,000-row
// result). Single-reference CTEs inline by default on PostgreSQL 12+, so
// the MATERIALIZED markers are load-bearing. OFFSET 0 keeps the lateral
// subquery from being flattened into a scan. The writer serializes
// package_id, repository_id and subject_digest on every finding, so the
// equality probe cannot miss a row on a NULL key.
//
// The remaining floor is one index descent plus the kept facts' heap rows per
// identifier, held in narrow tuplestore until the final fetch, plus the
// tick-invariant live_gens build over all live scopes (~150 ms cold on the
// QA replica, ~1% of the 15 s bar; buffer-resident warm expected). Re-open
// tick-scoping if the warm floor intercept ever exceeds 500 ms p95 or live
// scopes pass 100k (#10839 keeps the post-deploy intercept).
// winnersKeyedNarrowTable is the transaction-scoped probe output one keyed
// tick or audit range builds before the keyed active_facts, so the
// scanned-copies count reads rows instead of re-running the probe.
const winnersKeyedNarrowTable = "winners_keyed_narrow"

// winnersKeyedBatchIdentsTable stages the one identifier the budgeted probe
// admits next (Dec 6(ii)): probing from a one-row table keeps the shipped
// lateral shape (same 291 index descent, same MATERIALIZED and OFFSET 0
// guards) while the Go admission loop meters the scanned-copies budget one
// identifier at a time.
const winnersKeyedBatchIdentsTable = "winners_keyed_batch_idents"

// createWinnersKeyedBatchIdentsSQL stages the next identifier's five-tuple
// for the budgeted probe. Session-local to the tick transaction, like every
// chunk table here.
const createWinnersKeyedBatchIdentsSQL = `
CREATE TEMP TABLE winners_keyed_batch_idents (
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text
) ON COMMIT DROP
`

// deleteWinnersKeyedBatchIdentsSQL empties the staging table before the next
// identifier is staged, so each budgeted probe reads exactly one identifier.
const deleteWinnersKeyedBatchIdentsSQL = `DELETE FROM ` + winnersKeyedBatchIdentsTable

// insertWinnersKeyedBatchIdentPrefix heads the single-row INSERT that stages
// the next identifier. One row never approaches the parameter ceiling; the
// fail-closed batch builder below still packs it, so the staged columns
// cannot drift from the chunk's.
const insertWinnersKeyedBatchIdentPrefix = `INSERT INTO ` + winnersKeyedBatchIdentsTable + ` VALUES `

// winnersKeyedNarrowColumns is the probe output column list: the copy
// identity the active-facts build joins on. The narrow CREATE below declares
// these same three columns (the DDL parity scan reads only literal SQL);
// TestWinnersScannedCopiesProbeMatchesActiveFactsProbe pins the agreement.
const winnersKeyedNarrowColumns = `fact_id, scope_id, generation_id`

// winnersKeyedNarrowProbeSelect renders the identifier probe body over an
// identifier source table: one row per pre-filter copy. The MATERIALIZED
// marker and OFFSET 0 guard are load-bearing (an unguarded single-reference
// CTE inlines and flattens). Every probe shape derives from this one
// function, so the tick's budgeted per-identifier probes, the audit's
// whole-chunk build, and the scanned-copies count attribution cannot
// silently split (see TestWinnersScannedCopiesProbeMatchesActiveFactsProbe).
func winnersKeyedNarrowProbeSelect(identsTable string) string {
	return `WITH narrow AS MATERIALIZED (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id
    FROM ` + identsTable + ` AS ident
    CROSS JOIN LATERAL (
        SELECT f.fact_id, f.scope_id, f.generation_id
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
SELECT ` + winnersKeyedNarrowColumns + ` FROM narrow
`
}

// buildWinnersKeyedNarrowSQL persists the identifier probe over the whole
// identifier set: one row per pre-filter copy. The audit sweep, the staged
// ident build, and the EXPLAIN export run this whole-chunk shape; the keyed
// tick instead accumulates budgeted per-identifier probes below (same body,
// staged source table). analyzeWinnersKeyedNarrowSQL plans the final fetch
// on real statistics. The text is byte-identical to the pre-budget
// statement: it stays in the winners contract pin unchanged.
var buildWinnersKeyedNarrowSQL = `
CREATE TEMP TABLE winners_keyed_narrow ON COMMIT DROP AS
` + winnersKeyedNarrowProbeSelect(winnersKeyedIdentsTable)

// createWinnersKeyedNarrowSQL creates the budgeted probe's empty narrow
// table before the per-identifier accumulation fills it. Literal SQL (the
// DDL parity scan reads only literals): the three TEXT columns are the
// fact_records types of the probe output, pinned to
// winnersKeyedNarrowColumns by
// TestWinnersScannedCopiesProbeMatchesActiveFactsProbe.
const createWinnersKeyedNarrowSQL = `
CREATE TEMP TABLE winners_keyed_narrow (
    fact_id text, scope_id text, generation_id text
) ON COMMIT DROP
`

// insertWinnersKeyedNarrowBatchSQL probes exactly the staged identifier and
// appends its copies to the narrow table. RowsAffected is that identifier's
// scanned-copy count: the admission loop meters the scanned-copies budget
// from it, so the probe runs once per identifier and no second count
// statement re-reads the probe. The body is the same supports-swapped probe
// the whole-chunk build runs (#10880 PR-C1), staged to the batch identifier
// table, so the budgeted tick and the audit sweep cannot silently split.
// applyWinnersSupportsInputSwaps panics at init unless each legacy fragment
// matches exactly once.
var insertWinnersKeyedNarrowBatchSQL = `INSERT INTO ` + winnersKeyedNarrowTable + `
` + applyWinnersSupportsInputSwaps("keyed narrow probe (budgeted per-identifier)", winnersKeyedNarrowProbeSelect(winnersKeyedBatchIdentsTable), winnersKeyedActiveSwaps)

// analyzeWinnersKeyedNarrowSQL gives the planner the real probe output size.
const analyzeWinnersKeyedNarrowSQL = `ANALYZE ` + winnersKeyedNarrowTable

const buildWinnersKeyedActiveFactsSQL = `
CREATE TEMP TABLE winners_keyed_active_facts ON COMMIT DROP AS
WITH live_gens AS MATERIALIZED (
    SELECT s.scope_id, s.active_generation_id AS generation_id
    FROM ingestion_scopes AS s
    JOIN scope_generations AS generation
      ON generation.scope_id = s.scope_id
     AND generation.generation_id = s.active_generation_id
    WHERE generation.status = 'active'
)
SELECT fact.*
FROM fact_records AS fact
JOIN ` + winnersKeyedNarrowTable + ` AS n ON n.fact_id = fact.fact_id
JOIN live_gens AS g
  ON g.scope_id = n.scope_id
 AND g.generation_id = n.generation_id
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
// identifier probe scanned: every finding the five-tuple probe matches, before
// the live-generation semi-join keeps the live ones (#11308). It reads the
// narrow temp table the build persisted instead of re-running the probe, so a
// keyed tick probes once: the build and this count read the same rows from
// the same transaction and snapshot, and the tick writes no fact_records
// between them, so the count is exact. Under #10880 PR-C1 the narrow table
// holds the supports probe output, so the count reads no fact_records either.
// It is deliberately not part of the
// contract pin: it reads nothing the resweep writes.
const countWinnersKeyedScannedCopiesSQL = `
SELECT count(*) FROM ` + winnersKeyedNarrowTable + `
`

// winnersKeyedSelectSQL lives in supply_chain_impact_winners_supports_input.go:
// the narrow stage-1 rows streamed through the derived keyed body.

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
