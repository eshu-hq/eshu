// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
)

// SQL of the single-scan staged winners audit range (#10972). It keeps the
// shipped #11131 range contract (one budgeted read per range, cut only at
// identifier edges, oversize identifiers alone and whole, the last range
// open-ended on both sides, operator facts whole on every range) while the
// fact heap is read narrowly and once per kept row:
//
//   - range_raw projects only fact_id, scope_id, generation_id and the five
//     keyed-index identifier expressions, byte-identical to migration 291,
//     so the predicate and the ORDER BY stay index conditions and index
//     order. No payload, no source_confidence.
//   - range_bounds folds the shipped range_tail, range_below_tail and count
//     into one MATERIALIZED CTE: one window pass over range_raw computes the
//     row count, the rank in identifier order, and the below-tail rank, and
//     the outer aggregation keeps the shipped is_last, oversize and hi_*
//     truth tables. Rank selection and row-comparison selection agree
//     wherever the sweep is valid (a clean NULL probe, which gates every
//     sweep, means range_raw carries no NULL identifier fields); see the
//     knife-edge live proof for the one divergence, where the shipped pair
//     errors and this pair completes.
//   - range_facts re-attaches payload and source_confidence for the
//     non-oversize arm by primary-key join on fact_id in the same statement
//     and snapshot. The oversize arm is textually unchanged from shipped.
//
// Keep audit_range.go for the shared types, reader, and NULL probe; this
// file owns only the single-scan renderer and the staged-audit observation.

// renderWinnersAuditRangeSingleScanSQL renders the single-scan range
// statement. first renders the sweep's opening range, which has no lower
// bound and takes only $1.
func renderWinnersAuditRangeSingleScanSQL(first bool) string {
	factRow := winnersAuditRangeRow(winnersAuditRangeFactIdentExprs)
	storedRow := winnersAuditRangeRow(winnersAuditRangeStoredIdentExprs)
	cursorRow := winnersAuditRangeRow(winnersAuditRangeCursorParams)
	factLower, storedLower := "", ""
	if !first {
		factLower = "\n      AND " + factRow + " > " + cursorRow
		storedLower = storedRow + " > " + cursorRow + "\n      AND "
	}
	rawIdent := winnersAuditRangeRow(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns))
	boundRow := winnersAuditRangeRow(winnersAuditRangeBoundSubselects())

	identSelect := make([]string, len(winnersAuditRangeFactIdentExprs))
	for i, expr := range winnersAuditRangeFactIdentExprs {
		identSelect[i] = expr + " AS " + winnersAuditRangeIdentColumns[i]
	}
	oversizeMatch := make([]string, len(winnersAuditRangeFactIdentExprs))
	for i, expr := range winnersAuditRangeFactIdentExprs {
		oversizeMatch[i] = expr + " = " + winnersAuditRangeBoundSubselects()[i]
	}
	// orderDesc is the shipped tail ordering, reused for the rank window so
	// rank 1 is the shipped tail identifier and ranks above 1 are strictly
	// below it, including under the database collation.
	orderDesc := winnersAuditRangeOrder(winnersAuditRangeIdentColumns, " DESC")
	identCols := strings.Join(winnersAuditRangeIdentColumns, ", ")
	rankedIdentCols := strings.Join(winnersAuditRangePrefixed("ranked.", winnersAuditRangeIdentColumns), ", ")
	hiSelect := make([]string, len(winnersAuditRangeIdentColumns))
	for i, column := range winnersAuditRangeIdentColumns {
		// Rows sharing the below-tail rank share the whole tuple (rank is
		// over the full identifier order), and rows at rank 1 share the
		// tail tuple, so per-column maxima stay tuple-preserving. r0 is
		// NULL when no row sits below the tail, and the COALESCE then
		// keeps the shipped hi-equals-tail shape.
		hiSelect[i] = "COALESCE(max(w." + column + ") FILTER (WHERE w.rnk = w.r0), max(w." + column + ") FILTER (WHERE w.rnk = 1)) AS " + winnersAuditRangeBoundColumns[i]
	}
	pipe := make([]string, len(winnersAuditRangeIdentColumns))
	for i, column := range winnersAuditRangeIdentColumns {
		pipe[i] = "strpos(" + column + ", '|') > 0"
	}

	return `WITH range_raw AS MATERIALIZED (
    SELECT f.fact_id, f.scope_id, f.generation_id,
        ` + strings.Join(identSelect, ",\n        ") + `
    FROM fact_records AS f
    WHERE f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE` + factLower + `
    ORDER BY ` + strings.Join(winnersAuditRangeFactIdentExprs, ", ") + `
    LIMIT $1
),
range_bounds AS MATERIALIZED (
    SELECT
        COALESCE(max(w.n), 0) AS n,
        COALESCE(max(w.n), 0) < $1 AS is_last,
        (COALESCE(max(w.n), 0) >= $1 AND max(w.r0) IS NULL) AS oversize,
        ` + strings.Join(hiSelect, ",\n        ") + `
    FROM (
        SELECT ranked.n, ranked.rnk, ` + rankedIdentCols + `,
            min(CASE WHEN ranked.rnk > 1 THEN ranked.rnk END) OVER () AS r0
        FROM (
            SELECT count(*) OVER () AS n,
                rank() OVER (ORDER BY ` + orderDesc + `) AS rnk,
                ` + identCols + `
            FROM range_raw
        ) AS ranked
    ) AS w
),
range_facts AS MATERIALIZED (
    SELECT r.fact_id, r.scope_id, r.generation_id, f.payload, f.source_confidence, ` + strings.Join(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns), ", ") + `
    FROM range_raw AS r
    JOIN fact_records AS f ON f.fact_id = r.fact_id
    WHERE (SELECT NOT oversize FROM range_bounds)
      AND ((SELECT is_last FROM range_bounds) OR ` + rawIdent + ` <= ` + boundRow + `)
    UNION ALL
    SELECT f.fact_id, f.scope_id, f.generation_id, f.payload, f.source_confidence,
        ` + strings.Join(identSelect, ",\n        ") + `
    FROM fact_records AS f
    WHERE (SELECT oversize FROM range_bounds)
      AND f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE
      AND ` + strings.Join(oversizeMatch, "\n      AND ") + `
),
winners_now AS (
WITH active_facts AS NOT MATERIALIZED (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.payload, fact.source_confidence
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
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.payload, fact.source_confidence
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
    WHERE ` + storedLower + `(SELECT is_last FROM range_bounds)
    UNION ALL
    SELECT w.*
    FROM supply_chain_impact_canonical_winners AS w
    WHERE ` + storedLower + `NOT (SELECT is_last FROM range_bounds)
      AND ` + storedRow + ` <= ` + boundRow + `
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

// selectWinnersAuditSingleScanFirstRangeSQL opens a sweep with the
// single-scan statement: no lower bound, $1 = budget.
var selectWinnersAuditSingleScanFirstRangeSQL = renderWinnersAuditRangeSingleScanSQL(true)

// selectWinnersAuditSingleScanNextRangeSQL continues a single-scan sweep
// after the cursor $2..$6, $1 = budget.
var selectWinnersAuditSingleScanNextRangeSQL = renderWinnersAuditRangeSingleScanSQL(false)

// StagedAuditRangeObservation is the per-range read observation of the
// single-scan staged audit (#10972): the budgeted raw index rows the
// statement read against the recomputed facts.
type StagedAuditRangeObservation struct {
	// Budget is the row budget the range was read with.
	Budget int
	// RawRows is the budgeted index rows the statement read.
	RawRows int64
	// Facts is the fact rows the range recomputed.
	Facts int64
	// Oversize reports a range that is one identifier holding at least the
	// budget in facts; it ran alone and whole.
	Oversize bool
	// Last reports the range that reached the end of the index.
	Last bool
}

// OverFetchRatio reports raw over facts: how many budgeted index rows the
// range read per recomputed fact. A range that recomputed nothing reports
// zero instead of dividing by zero.
func (o StagedAuditRangeObservation) OverFetchRatio() float64 {
	if o.Facts <= 0 {
		return 0
	}
	return float64(o.RawRows) / float64(o.Facts)
}

// report renders the observation as one operator-readable line, carrying
// the raw/facts over-fetch ratio.
func (o StagedAuditRangeObservation) report() string {
	return fmt.Sprintf("staged audit range budget=%d raw=%d facts=%d over_fetch=%.2f oversize=%t last=%t",
		o.Budget, o.RawRows, o.Facts, o.OverFetchRatio(), o.Oversize, o.Last)
}

// ObserveRange builds the staged-audit observation for one read range and
// emits its report line. The caller logs the line on the sweep path; the
// returned observation carries the raw rows beside the facts for
// aggregation.
func ObserveRange(rng WinnersAuditRange) (StagedAuditRangeObservation, string) {
	obs := StagedAuditRangeObservation{
		Budget:   rng.Budget,
		RawRows:  rng.RawRows,
		Facts:    rng.Facts,
		Oversize: rng.Oversize,
		Last:     rng.Last,
	}
	return obs, obs.report()
}
