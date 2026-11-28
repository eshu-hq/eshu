// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

// Apply of the keyed resweep (#9805): recompute the identifier set's winners
// over the keyed active_facts, write only new or changed rows, delete stored
// winners of the set that are no longer produced, and stamp the watermark.
// Moved verbatim from supply_chain_impact_winners_keyed_sql.go for the
// 500-line file cap; behavior-preserving move, no statement text changed.

// winnersStoredIdentC0 renders the coalesced fourth identifier over the
// stored winner columns of alias. The keyed delete match and the full
// enumeration's stored arm both derive it from here, so a drift between the
// two reads as a hermetic failure, never a silent missed delete.
func winnersStoredIdentC0(alias string) string {
	return `COALESCE(NULLIF(` + alias + `.cve_id, ''), NULLIF(` + alias + `.advisory_id, ''), '')`
}

// winnersStoredIdentTuple renders the five stored identifier expressions
// over alias, in identifier-table column order.
func winnersStoredIdentTuple(alias string) string {
	return alias + `.package_id, ` + alias + `.repository_id, ` + alias + `.subject_digest,
        ` + winnersStoredIdentC0(alias) + `, ` + alias + `.advisory_id`
}

// winnersStoredIdentSelectList renders the five stored identifier columns
// with the identifier-table column names, for a SELECT over the stored
// winners table. It shares winnersStoredIdentC0 with the match tuple, so
// the enumeration's stored arm cannot drift from the delete's identity.
func winnersStoredIdentSelectList(alias string) string {
	return alias + `.package_id AS package_id,
        ` + alias + `.repository_id AS repository_id,
        ` + alias + `.subject_digest AS subject_digest,
        ` + winnersStoredIdentC0(alias) + ` AS c0,
        ` + alias + `.advisory_id AS advisory_id`
}

// winnersStoredIdentMatch matches stored winner rows (alias w) to the
// identifier table; the winners table keeps the same coalesced columns. It
// stays a literal so const SQL builders (the enrichment carry claim) can
// splice it; TestWinnersStoredIdentMatchDerivesFromSharedFragment pins it
// byte-equal to the derived tuple, so the literal cannot drift from the
// fragment the full enumeration's stored arm derives from.
const winnersStoredIdentMatch = `(w.package_id, w.repository_id, w.subject_digest,
        COALESCE(NULLIF(w.cve_id, ''), NULLIF(w.advisory_id, ''), ''), w.advisory_id)
       IN (SELECT package_id, repository_id, subject_digest, c0, advisory_id FROM ` + winnersKeyedIdentsTable + `)`

// applyWinnersKeyedSQL recomputes the identifier set's winners and writes only
// the rows that are new or changed, deletes stored winners of the identifier
// set that are no longer produced, and stamps the watermark. It is the keyed
// twin of the chunked full rebuild (applyWinnersFullChunkSQL): the same changed/upserted/deleted
// shape and the same row comparison, restricted to the identifier set. $1 is
// the stamp candidate: the DB-clock tick stamp S when the log drained, or
// the horizon H = min(captured_at) of the remainder minus 1us when rows are
// still queued (#10706). The stamp is GREATEST(previous, candidate), so the
// watermark never regresses. It returns inserted, updated, deleted,
// unchanged, the keys recomputed (winners now produced plus winners deleted),
// and the post-stamp watermark (scanned for arity only; the replaced value
// is pre-read by the caller, #11515).
// applyWinnersKeyedStampTail is the shared stamp and counts tail of a keyed
// apply: the watermark upsert plus the inserted/updated/deleted/unchanged/
// keys counts over the changed, deleted, and winners_now CTEs. Both the
// legacy and the TOP-1 apply end in this text.
const applyWinnersKeyedStampTail = `
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

var applyWinnersKeyedSQL = applyWinnersKeyedBodySQL + applyWinnersKeyedStampTail

// applyWinnersKeyedTop1SQL is the TOP-1 keyed apply: the TOP-1 winner select
// through the identical write and stamp shape.
var applyWinnersKeyedTop1SQL = applyWinnersKeyedTop1BodySQL + applyWinnersKeyedStampTail

// winnersKeyedApplyBodySQLFor renders the keyed apply body over a winner
// select: the winners CTE plus the shared changed/upserted/deleted shape and
// the stamp candidate. The full-chunked rebuild derives its apply from the
// legacy body by swapping the winner select (a derivation that panics on any
// other drift), and the TOP-1 apply reuses the same shape over the TOP-1
// select, so the write semantics cannot split between paths.
func winnersKeyedApplyBodySQLFor(winnersSelect string) string {
	return `
WITH winners_now AS (` + winnersSelect + `),
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
}

// applyWinnersKeyedBodySQL is the legacy keyed apply body over the streamed
// winner select. It renders through the shared constructor so the TOP-1
// apply cannot drift from it; the full-chunked derivation and the contract
// pin read these bytes.
var applyWinnersKeyedBodySQL = winnersKeyedApplyBodySQLFor(winnersKeyedSelectSQL)

// applyWinnersKeyedTop1BodySQL is the TOP-1 keyed apply body: the identical
// changed/upserted/deleted shape and stamp candidate over the TOP-1 winner
// select.
var applyWinnersKeyedTop1BodySQL = winnersKeyedApplyBodySQLFor(winnersKeyedTop1SelectSQL)

// stampWinnersWatermarkSQL stamps the watermark candidate for a tick that
// consumed nothing: $1 is the DB-clock tick stamp S when the log is empty, or
// the horizon H = min(captured_at) minus 1us when rows landed after admission
// (#10706). A watermark that only moves when something changed would make an
// idle table read as stale. The stamp is GREATEST(previous, candidate), so the
// watermark never regresses; its RETURNING reads the post-upsert value, so the
// caller pre-reads the replaced value (#11515).
// #nosec G101 -- SQL statement text, not a credential.
const stampWinnersWatermarkSQL = `
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = GREATEST(supply_chain_impact_winners_materialization.materialized_at, EXCLUDED.materialized_at)
RETURNING (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
`
