// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// SQL for the winners-sourced supply-chain impact aggregate read model
// (#10880 slice 2b). The filtered finding set reads one row per canonical
// key from supply_chain_impact_canonical_winners instead of deduplicating
// reducer_supply_chain_impact_finding facts at read time. The filter
// predicates mirror ListFindingsFromWinnersQuery renumbered onto the
// aggregate argument order ($1..$20, see PostgresAggregateStore methods),
// the same order groupedInventoryFindingsCTE already uses.
//
// The caller-authorization arguments ($21..$26 on the legacy path) are not
// bound here: they feed only the derived protected-read branch, which the
// winners table never holds (static winners only, enforced by
// supplyChainImpactStaticWinnerPredicateSQL). This matches the grouped
// inventory winners read, which likewise binds no authorization arguments.
//
// Scoped-token grants ($18/$19) never reach these queries through the
// store: PostgresAggregateStore routes granted calls to the legacy CTE,
// mirroring the list read, because the winners table picks one winner per
// canonical key before the grant applies while the legacy CTE applies the
// grant before the pick.

// aggregateWinnersEffectiveStateSQL is the read-time effective suppression
// state of one winner: an operator decision whose expiry is at or before
// the single read clock ($20) reads 'expired', exactly as the winners list
// read and the grouped inventory winners read evaluate it.
var aggregateWinnersEffectiveStateSQL = supplyChainImpactEffectiveDecisionStateSQL(
	"w.suppression_state",
	"COALESCE(w.suppression_expires_at::text, '')",
	"$20::timestamptz",
)

// aggregateWinnersGrantSQL is the scoped-token grant predicate over a
// repository and scope expression, the same shape the runtime-filter CTE
// and the winners list read apply. Both arrays empty means unrestricted.
// The store only serves ungranted calls from these queries, so in
// production both arrays are always empty here; the predicate stays so the
// queries keep the full 20-argument shape and a granted call could never
// widen into an ungranted read by accident.
func aggregateWinnersGrantSQL(repositoryExpr, scopeExpr string) string {
	return `(
          (COALESCE(cardinality($18::text[]), 0) = 0 AND COALESCE(cardinality($19::text[]), 0) = 0)
          OR ` + repositoryExpr + ` = ANY($18::text[])
          OR ` + scopeExpr + ` = ANY($19::text[])
        )`
}

// aggregateWinnersFindingsCTE selects the filtered finding set from the
// maintained winners table: one row per canonical key, so a COUNT over it
// is the /impact/count total for the same filters.
var aggregateWinnersFindingsCTE = `
aggregate_winners AS NOT MATERIALIZED (
  SELECT w.impact_status,
         w.priority_bucket,
         w.severity_bucket,
         w.repository_id,
         w.ecosystem
  FROM supply_chain_impact_canonical_winners AS w
  WHERE TRUE
    -- The six caller-authorization arguments ($21..$26) feed only the
    -- derived protected-read branch, which the winners table never holds.
    -- They are referenced here, as in ListFindingsFromWinnersQuery, so the
    -- store can swap queries without rebuilding args and every bound
    -- parameter stays used; all six are non-null by construction.
    AND $21::boolean IS NOT NULL
    AND $22::text IS NOT NULL
    AND $23::text IS NOT NULL
    AND $24::text IS NOT NULL
    AND $25::text IS NOT NULL
    AND $26::text[] IS NOT NULL
` + supplyChainImpactStaticWinnerPredicateSQL + `
    AND ($1 = '' OR w.cve_id = $1)
    AND ($2 = '' OR w.package_id = $2)
    AND ($3 = '' OR w.repository_id = $3)
    AND ($4 = '' OR w.subject_digest = $4)
    AND ($5 = '' OR w.impact_status = $5)
    AND ($6 = '' OR w.advisory_id = $6)
    AND ($7 = '' OR LOWER((w.ecosystem) COLLATE "C") COLLATE "default" = LOWER(($7) COLLATE "C") COLLATE "default")
` + supplyChainImpactRuntimeFilterPredicate("w.repository_id", "$8", "$9", "$10") + `
    AND ($11 = '' OR w.severity_bucket = $11)
    AND (
          $12 = ''
          OR w.detection_profile = $12
          OR ($12 = 'comprehensive' AND COALESCE(w.detection_profile, '') = '')
          OR (
                $12 = 'precise'
                AND COALESCE(w.detection_profile, '') = ''
                AND w.impact_status IN ('affected_exact', 'not_affected_known_fixed')
                AND COALESCE(w.observed_version, '') <> ''
                AND w.match_reason IN (
                      'npm_semver_affected_range', 'npm_semver_known_fixed',
                      'nuget_semver_affected_range', 'nuget_semver_known_fixed',
                      'cargo_semver_affected_range', 'cargo_semver_known_fixed',
                      'hex_semver_affected_range', 'hex_semver_known_fixed',
                      'maven_range_match', 'maven_known_fixed',
                      'swift_semver_affected_range', 'swift_semver_known_fixed'
                    )
             )
        )
    AND ($13 = '' OR w.priority_bucket = $13)
    AND ($14 = 0 OR w.priority_score >= $14)
    AND ($15 = '' OR ` + aggregateWinnersEffectiveStateSQL + ` = $15)
    AND (
          $16::boolean
          OR ` + aggregateWinnersEffectiveStateSQL + ` NOT IN (` + supplyChainImpactHiddenSuppressionStatesSQL + `)
        )
    AND ($17 = '' OR w.image_ref = $17)
    AND ` + aggregateWinnersGrantSQL("w.repository_id", "w.winner_scope_id") + `
)`

// aggregateWinnersFromClause opens every winners aggregate query on the
// runtime-filter CTE the predicate above needs plus the filtered set. The
// CTE legs read reducer workload-identity, service-catalog, and CI/CD
// correlation facts (other reducer families, not the finding family), so
// they stay on fact_records while the finding set moves.
func aggregateWinnersFromClause() string {
	// The protected-reads marker (inside the static-winner predicate)
	// substitutes closed, as the grouped inventory winners read does:
	// aggregates never take the derived protected branch.
	return supplyChainImpactReadModeSQL(
		`WITH `+supplyChainImpactRuntimeFilterCTE("$8", "$9", "$10", "$18", "$19")+`,`+aggregateWinnersFindingsCTE,
		false,
	)
}

// AggregateWinnersCombinedCountQuery returns the full count envelope (status
// totals plus priority and severity buckets) over the winners-sourced
// canonical set in one Postgres statement, the winners-side twin of
// AggregateCombinedCountQuery (#10970). aggregate_winners is NOT
// MATERIALIZED, so it is scanned once per output column; the winners set is
// one row per canonical key behind a selective index, so no dedup sort is
// repeated. Buckets keep the same unknown/none defaults as the legacy
// facet queries, and the column order matches the legacy combined query so
// one scan serves both.
var AggregateWinnersCombinedCountQuery = aggregateWinnersFromClause() + `
SELECT
	(SELECT COUNT(*) FROM aggregate_winners) AS total,
	(SELECT COUNT(*) FROM aggregate_winners WHERE impact_status IN ('affected_exact', 'affected_derived', 'possibly_affected')) AS affected,
	(SELECT COUNT(*) FROM aggregate_winners WHERE impact_status = 'affected_exact') AS affected_exact,
	(SELECT COUNT(*) FROM aggregate_winners WHERE impact_status = 'affected_derived') AS affected_derived,
	(SELECT COUNT(*) FROM aggregate_winners WHERE impact_status = 'possibly_affected') AS possibly_affected,
	(SELECT COUNT(*) FROM aggregate_winners WHERE impact_status LIKE 'not_affected%') AS not_affected,
	(SELECT COALESCE(jsonb_agg(jsonb_build_object('bucket', bucket, 'bucket_count', bucket_count)), '[]'::jsonb)
	 FROM (SELECT COALESCE(NULLIF(priority_bucket, ''), 'unknown') AS bucket, COUNT(*) AS bucket_count
	       FROM aggregate_winners GROUP BY bucket) AS priority_buckets) AS priority_buckets,
	(SELECT COALESCE(jsonb_agg(jsonb_build_object('bucket', bucket, 'bucket_count', bucket_count)), '[]'::jsonb)
	 FROM (SELECT COALESCE(NULLIF(severity_bucket, ''), 'none') AS bucket, COUNT(*) AS bucket_count
	       FROM aggregate_winners GROUP BY bucket) AS severity_buckets) AS severity_buckets;
`

// AggregateWinnersInventoryQueryTemplate groups the winners-sourced
// canonical set by a caller-selected dimension. The group-expression
// placeholder substitutes one of the InventoryBy* SQL expressions, which
// read the `fact` alias the final select below declares.
var AggregateWinnersInventoryQueryTemplate = aggregateWinnersFromClause() + `
SELECT ` + supplyChainImpactInventoryGroupExpressionPlaceholder + ` AS bucket, COUNT(*) AS bucket_count
FROM aggregate_winners AS fact
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $27 OFFSET $28;
`

// AggregateWinnersInventoryQuery inserts one expression selected by the
// closed InventoryDimension enum. A sentinel replacement is deliberate:
// fmt.Sprintf over the complete SQL would reinterpret percent literals in
// runtime repository-scope LIKE patterns.
func AggregateWinnersInventoryQuery(groupExpr string) string {
	return strings.Replace(
		AggregateWinnersInventoryQueryTemplate,
		supplyChainImpactInventoryGroupExpressionPlaceholder,
		groupExpr,
		1,
	)
}
