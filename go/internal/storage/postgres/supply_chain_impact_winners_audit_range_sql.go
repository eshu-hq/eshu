// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

// SQL of the staged winners audit sweep (#11131). The sweep replaces the
// whole-corpus staged audit tick with a walk over the keyed winners index
// (migration 291) in its key order: package_id, repository_id,
// subject_digest, c0, advisory_id. One range is one standby statement and one
// snapshot. It takes the next B index rows after the cursor, trims the last
// identifier when the budget cut through it, recomputes the winners of the
// identifiers it kept with the shipped winner body, and diffs them against the
// stored winners in the same key interval.
//
// The interval rules, each pinned by the T3 differential:
//   - A range is cut only at identifier edges, so a canonical key never spans
//     two ranges (every identifier field is a canonical-key component, and the
//     '|' guard below keeps the key-to-identifier map a function).
//   - An identifier with more than B facts runs alone and whole (oversize).
//   - The first range has no lower bound; the last range (fewer than B rows)
//     has no upper bound on either side, because stored winners whose facts
//     are gone can sort after every fact.
//   - Operator-suppression facts load whole on every range: they can lack the
//     identifier fields, and the shipped body joins their overrides by
//     canonical key, so only source keys of this range pick one up.

// winnersAuditRangeFactIdentExprs are the five keyed-index key expressions
// over alias f, byte-identical to migration 291 so the range predicate and the
// ORDER BY are index conditions and index order.
var winnersAuditRangeFactIdentExprs = []string{
	`(f.payload->>'package_id')`,
	`(f.payload->>'repository_id')`,
	`(f.payload->>'subject_digest')`,
	`COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '')`,
	`COALESCE(f.payload->>'advisory_id', '')`,
}

// winnersAuditRangeStoredIdentExprs are the same five identifiers over a
// stored winner row (alias w). The stored columns hold the coalesced payload
// values the winner body projected, so for a key whose facts carry no NULL
// identifier field (the NULL probe's guarantee) both sides compare equal.
var winnersAuditRangeStoredIdentExprs = []string{
	`w.package_id`,
	`w.repository_id`,
	`w.subject_digest`,
	`COALESCE(NULLIF(w.cve_id, ''), NULLIF(w.advisory_id, ''), '')`,
	`w.advisory_id`,
}

// winnersAuditRangeIdentColumns name the identifier columns the range CTEs
// carry, in key order.
var winnersAuditRangeIdentColumns = []string{"i_package_id", "i_repository_id", "i_subject_digest", "i_c0", "i_advisory_id"}

// winnersAuditRangeBoundColumns name the new-cursor columns of range_bounds.
var winnersAuditRangeBoundColumns = []string{"hi_package_id", "hi_repository_id", "hi_subject_digest", "hi_c0", "hi_advisory_id"}

func winnersAuditRangePrefixed(prefix string, columns []string) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = prefix + column
	}
	return out
}

func winnersAuditRangeRow(exprs []string) string {
	return "ROW(" + strings.Join(exprs, ", ") + ")"
}

func winnersAuditRangeOrder(exprs []string, direction string) string {
	out := make([]string, len(exprs))
	for i, expr := range exprs {
		out[i] = expr + direction
	}
	return strings.Join(out, ", ")
}

// winnersAuditRangeCursorParams are the cursor parameters $2..$6; $1 is the
// row budget in both variants.
var winnersAuditRangeCursorParams = []string{"$2::text", "$3::text", "$4::text", "$5::text", "$6::text"}

// winnersAuditRangeBoundSubselects read the new cursor from range_bounds as
// InitPlan parameters, so they can be index conditions.
func winnersAuditRangeBoundSubselects() []string {
	out := make([]string, len(winnersAuditRangeBoundColumns))
	for i, column := range winnersAuditRangeBoundColumns {
		out[i] = "(SELECT " + column + " FROM range_bounds)"
	}
	return out
}

// renderWinnersAuditRangeSQL renders the range statement. first renders the
// sweep's opening range, which has no lower bound and takes only $1.
func renderWinnersAuditRangeSQL(first bool) string {
	factRow := winnersAuditRangeRow(winnersAuditRangeFactIdentExprs)
	storedRow := winnersAuditRangeRow(winnersAuditRangeStoredIdentExprs)
	cursorRow := winnersAuditRangeRow(winnersAuditRangeCursorParams)
	factLower, storedLower := "", ""
	if !first {
		factLower = "\n      AND " + factRow + " > " + cursorRow
		storedLower = storedRow + " > " + cursorRow + "\n      AND "
	}
	rawIdent := winnersAuditRangeRow(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns))
	tailIdent := winnersAuditRangeRow(winnersAuditRangePrefixed("t.", winnersAuditRangeIdentColumns))
	boundRow := winnersAuditRangeRow(winnersAuditRangeBoundSubselects())

	identSelect := make([]string, len(winnersAuditRangeFactIdentExprs))
	for i, expr := range winnersAuditRangeFactIdentExprs {
		identSelect[i] = expr + " AS " + winnersAuditRangeIdentColumns[i]
	}
	oversizeMatch := make([]string, len(winnersAuditRangeFactIdentExprs))
	for i, expr := range winnersAuditRangeFactIdentExprs {
		oversizeMatch[i] = expr + " = " + winnersAuditRangeBoundSubselects()[i]
	}
	hiSelect := make([]string, len(winnersAuditRangeIdentColumns))
	for i, column := range winnersAuditRangeIdentColumns {
		hiSelect[i] = "CASE WHEN b.present THEN b." + column + " ELSE t." + column + " END AS " + winnersAuditRangeBoundColumns[i]
	}
	pipe := make([]string, len(winnersAuditRangeIdentColumns))
	for i, column := range winnersAuditRangeIdentColumns {
		pipe[i] = "strpos(" + column + ", '|') > 0"
	}
	identCols := strings.Join(winnersAuditRangeIdentColumns, ", ")

	return `WITH range_raw AS MATERIALIZED (
    SELECT f.fact_id, f.scope_id, f.generation_id, f.payload,
        ` + strings.Join(identSelect, ",\n        ") + `
    FROM fact_records AS f
    WHERE f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE` + factLower + `
    ORDER BY ` + strings.Join(winnersAuditRangeFactIdentExprs, ", ") + `
    LIMIT $1
),
range_tail AS MATERIALIZED (
    SELECT ` + identCols + `
    FROM range_raw
    ORDER BY ` + winnersAuditRangeOrder(winnersAuditRangeIdentColumns, " DESC") + `
    LIMIT 1
),
range_below_tail AS MATERIALIZED (
    SELECT TRUE AS present, ` + strings.Join(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns), ", ") + `
    FROM range_raw AS r, range_tail AS t
    WHERE ` + rawIdent + ` < ` + tailIdent + `
    ORDER BY ` + winnersAuditRangeOrder(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns), " DESC") + `
    LIMIT 1
),
range_bounds AS MATERIALIZED (
    SELECT c.n,
        c.n < $1 AS is_last,
        (c.n >= $1 AND b.present IS NULL) AS oversize,
        ` + strings.Join(hiSelect, ",\n        ") + `
    FROM (SELECT count(*) AS n FROM range_raw) AS c
    LEFT JOIN range_tail AS t ON TRUE
    LEFT JOIN range_below_tail AS b ON TRUE
),
range_facts AS MATERIALIZED (
    SELECT r.fact_id, r.scope_id, r.generation_id, r.payload, ` + strings.Join(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns), ", ") + `
    FROM range_raw AS r
    WHERE (SELECT NOT oversize FROM range_bounds)
      AND ((SELECT is_last FROM range_bounds) OR ` + rawIdent + ` <= ` + boundRow + `)
    UNION ALL
    SELECT f.fact_id, f.scope_id, f.generation_id, f.payload,
        ` + strings.Join(identSelect, ",\n        ") + `
    FROM fact_records AS f
    WHERE (SELECT oversize FROM range_bounds)
      AND f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE
      AND ` + strings.Join(oversizeMatch, "\n      AND ") + `
),
winners_now AS (
WITH active_facts AS NOT MATERIALIZED (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.payload
    FROM range_facts AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE generation.status = 'active'
      AND fact.scope_id <> '` + supplyChainImpactOperatorScopeID + `'
    UNION ALL
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.payload
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.scope_id = '` + supplyChainImpactOperatorScopeID + `'
      AND fact.fact_kind = 'reducer_supply_chain_impact_finding'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
)` + supplyChainImpactWinnerBodySQL + `
),
stored_range AS (
    SELECT w.*
    FROM supply_chain_impact_canonical_winners AS w
    WHERE ` + storedLower + `((SELECT is_last FROM range_bounds) OR ` + storedRow + ` <= ` + boundRow + `)
),
range_diff AS (
` + winnersAuditDiffSelect("winners_now", "stored_range") + `
)
SELECT 'b' AS row_kind, b.n::bigint AS n, b.is_last, b.oversize,
    ` + strings.Join(winnersAuditRangePrefixed("b.", winnersAuditRangeBoundColumns), ", ") + `,
    (SELECT count(*) FROM range_facts WHERE scope_id <> '` + supplyChainImpactOperatorScopeID + `')::bigint AS facts,
    COALESCE((SELECT bool_or(` + strings.Join(pipe, " OR ") + `) FROM range_facts
        WHERE scope_id <> '` + supplyChainImpactOperatorScopeID + `'), FALSE) AS pipe
FROM range_bounds AS b
UNION ALL
SELECT 'c', NULL::bigint, NULL::boolean, NULL::boolean,
    d.package_id, d.repository_id, d.subject_digest, d.c0, d.advisory_id,
    NULL::bigint, NULL::boolean
FROM range_diff AS d`
}

// selectWinnersAuditFirstRangeSQL opens a sweep: no lower bound, $1 = budget.
var selectWinnersAuditFirstRangeSQL = renderWinnersAuditRangeSQL(true)

// selectWinnersAuditNextRangeSQL continues a sweep after the cursor $2..$6,
// $1 = budget.
var selectWinnersAuditNextRangeSQL = renderWinnersAuditRangeSQL(false)
