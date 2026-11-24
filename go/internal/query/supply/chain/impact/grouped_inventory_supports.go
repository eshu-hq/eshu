// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "fmt"

// groupedSupportsSourceCTE is the grouped inventory's source CTE over the
// supports table: the aggregate bind order ($1-$20, grants at $18/$19) without
// the caller-authorization binds, because the grouped statement binds limit and
// offset at $21/$22 and the supports table holds no derived-class row.
var groupedSupportsBinds = supportsSourceBinds{
	columns: `fact.fact_id, fact.scope_id, fact.suppression_state, fact.priority_score,
         fact.has_payload_finding_id, fact.canonical_key, fact.impact_status,
         fact.priority_bucket, fact.severity_bucket, fact.repository_id, fact.ecosystem`,
	cve: "$1", pkg: "$2", repo: "$3", digest: "$4", status: "$5", advisory: "$6", ecosystem: "$7",
	service: "$8", workload: "$9", environment: "$10", profile: "$12", image: "$17",
	repoGrant: "$18", scopeGrant: "$19",
}
var groupedSupportsSourceCTE = supportsSourceCTE(groupedSupportsBinds)

// groupedSupportsCanonicalFactsCTE is the canonical-facts CTE of a granted
// grouped inventory call, the same derivation the granted aggregates use, so a
// bucket and DistinctFindings equal the count route for the same filters: the
// grant applies to the support row, the pick runs over the granted supports, and
// the rank filters apply to the picked row (arbiter ruling for #10880 PR-B1, Q2).
var groupedSupportsCanonicalFactsCTE = mustDeriveGroupedSupportsCTE()

func mustDeriveGroupedSupportsCTE() string {
	query, err := deriveSupportsCanonicalFactsCTE(AggregateCanonicalFactsCTE, groupedSupportsSourceCTE)
	if err != nil {
		panic(err)
	}
	return query
}

// groupedSupportsHiddenSQL marks a bucket partial when the grant hides one of
// its map rows, or any live static support on a repository the bucket maps to
// that the grant does not admit, independent of the request filters (a request
// filter feeding it would let a scoped caller watch a bucket flip between
// complete and partial, an oracle on hidden findings). It is a per-repository
// probe over the supports repository index, so it reads only the repositories the
// map already names. A support the grant hides while another copy of its key is
// granted still reads partial: the signal over-reports and discloses nothing
// about an individual finding.
const groupedSupportsHiddenSQL = `grouped_hidden AS MATERIALIZED (
  SELECT DISTINCT hidden_map.dim_id
  FROM grouped_map AS hidden_map
  WHERE NOT hidden_map.granted
     OR EXISTS (
          SELECT 1
          FROM supply_chain_impact_finding_supports AS hidden_support
          JOIN ingestion_scopes AS hidden_scope
            ON hidden_scope.scope_id = hidden_support.scope_id
           AND hidden_scope.active_generation_id = hidden_support.generation_id
          JOIN scope_generations AS hidden_generation
            ON hidden_generation.scope_id = hidden_support.scope_id
           AND hidden_generation.generation_id = hidden_support.generation_id
          LEFT JOIN scanner_image_result_fact_classifications AS hidden_class
            ON hidden_class.finding_fact_id = hidden_support.fact_id
          LEFT JOIN scanner_image_result_fact_associations AS hidden_association
            ON hidden_association.finding_fact_id = hidden_support.fact_id
          WHERE hidden_support.repository_id = hidden_map.repository_id
            AND hidden_support.repository_id <> ''
            AND hidden_generation.status = 'active'
            AND hidden_support.scope_id <> 'operator:vulnerability_suppressions'
            AND hidden_association.finding_fact_id IS NULL
            AND hidden_support.scope_id NOT LIKE 'image://derived/%'
            AND (hidden_class.finding_fact_id IS NULL
                 OR (hidden_class.source_class = 'static' AND hidden_class.lifecycle = 'active'))
            AND NOT COALESCE(` + groupedHiddenGrantSQL + `, FALSE)
        )
),`

// groupedHiddenGrantSQL is the grant predicate over the probed support row.
const groupedHiddenGrantSQL = `(
              (COALESCE(cardinality($18::text[]), 0) = 0 AND COALESCE(cardinality($19::text[]), 0) = 0)
              OR hidden_support.repository_id = ANY($18::text[])
              OR hidden_support.scope_id = ANY($19::text[])
            )`

// groupedSupportsScopedTailSQL is the grouped tail with the hidden-bucket CTE
// reading the supports table.
var groupedSupportsScopedTailSQL = groupedInventoryMustReplace(
	groupedInventoryTailSQL,
	`grouped_hidden AS MATERIALIZED (
  SELECT DISTINCT hidden_map.dim_id
  FROM grouped_map AS hidden_map
  WHERE NOT hidden_map.granted
),`,
	groupedSupportsHiddenSQL,
)

// groupedInventorySupportsQuery builds the granted grouped inventory query for
// one dimension: the supports canonical-facts CTE, then the winners-shaped
// fan-out and tail unchanged. It takes the same 22 arguments as
// groupedInventoryQuery.
func groupedInventorySupportsQuery(dimension GroupedInventoryDimension) (string, error) {
	mapRows, err := groupedInventoryMapRowsSQL(dimension)
	if err != nil {
		return "", fmt.Errorf("grouped inventory supports query: %w", err)
	}
	query := groupedSupportsCanonicalFactsCTE + `,
grouped_findings AS MATERIALIZED (
  SELECT repository_id, impact_status, severity_bucket, priority_score
  FROM canonical_facts
),
grouped_map_rows AS MATERIALIZED (` + mapRows + `
)` + groupedSupportsScopedTailSQL
	return supplyChainImpactReadModeSQL(query, false), nil
}
