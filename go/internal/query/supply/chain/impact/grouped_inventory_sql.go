// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "fmt"

// groupedInventoryArgCount is the bound-parameter count of every grouped
// inventory query: the twenty aggregate filter arguments in the aggregate
// store's order ($1..$20, see PostgresAggregateStore.SupplyChainImpactInventory)
// plus limit ($21) and offset ($22). The caller-authorization arguments the
// aggregate passes ($21..$26 there) are not bound: they feed only the derived
// protected-read branch, which the winners table never holds (static winners
// only, enforced by supplyChainImpactStaticWinnerPredicateSQL).
const groupedInventoryArgCount = 22

// groupedInventoryGrantSQL is the scoped-token grant predicate over a
// repository and scope expression, the same shape the runtime-filter CTE and
// the winners list read apply. Both arrays empty means unrestricted.
func groupedInventoryGrantSQL(repositoryExpr, scopeExpr string) string {
	return `(
          (COALESCE(cardinality($18::text[]), 0) = 0 AND COALESCE(cardinality($19::text[]), 0) = 0)
          OR ` + repositoryExpr + ` = ANY($18::text[])
          OR ` + scopeExpr + ` = ANY($19::text[])
        )`
}

// groupedInventoryEffectiveStateSQL is the read-time effective suppression
// state of one winner: an operator decision whose expiry is at or before the
// single read clock ($20) reads 'expired', exactly as the winners list read
// and the aggregate canonical-facts CTE evaluate it.
var groupedInventoryEffectiveStateSQL = supplyChainImpactEffectiveDecisionStateSQL(
	"w.suppression_state",
	"COALESCE(w.suppression_expires_at::text, '')",
	"$20::timestamptz",
)

// groupedInventoryFindingsCTE selects the filtered finding set from the
// maintained winners table: one row per canonical key, so a COUNT over it is
// the /impact/count total for the same filters. The predicates mirror
// ListFindingsFromWinnersQuery renumbered onto the aggregate argument order.
var groupedInventoryFindingsCTE = `
grouped_findings AS MATERIALIZED (
  SELECT w.repository_id,
         w.impact_status,
         w.severity_bucket,
         w.priority_score
  FROM supply_chain_impact_canonical_winners AS w
  WHERE TRUE
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
    AND ($15 = '' OR ` + groupedInventoryEffectiveStateSQL + ` = $15)
    AND (
          $16::boolean
          OR ` + groupedInventoryEffectiveStateSQL + ` NOT IN (` + supplyChainImpactHiddenSuppressionStatesSQL + `)
        )
    AND ($17 = '' OR w.image_ref = $17)
    AND ` + groupedInventoryGrantSQL("w.repository_id", "w.winner_scope_id") + `
)`

// groupedInventoryMapLeg renders one current runtime-fact leg of the
// repository-to-group map: active generation, not tombstoned, the repository
// anchor decoded by the same decoder the runtime-filter CTE uses, and the
// group value BTRIM-ed (the filter legs match the exact value or its BTRIM, so
// a trimmed bucket value X returns exactly the repositories filter=X returns).
// extraFrom adds a lateral source before the decoder join; where carries the
// fact kind and value guards.
func groupedInventoryMapLeg(dimExpr, extraFrom, where string) string {
	return `
  SELECT runtime_repository.repository_id,
         runtime_fact.scope_id,
         ` + dimExpr + ` AS dim_id
  FROM fact_records AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id` + extraFrom +
		supplyChainRuntimeRepositoryDecoderJoin("runtime_fact.payload", "runtime_fact.scope_id", "runtime_repository") + `
  WHERE runtime_fact.is_tombstone = FALSE
    AND runtime_generation.status = 'active'` + where
}

// groupedInventoryAcceptedCorrelationSQL is the outcome guard the
// runtime-filter CTE applies to catalog and CI/CD correlation facts.
const groupedInventoryAcceptedCorrelationSQL = `
    AND BTRIM(COALESCE(runtime_fact.payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(runtime_fact.payload->>'provenance_only', ''))) <> 'true'`

// groupedInventoryStringFieldLeg is a map leg whose group value is one string
// payload field of fact kind kind.
func groupedInventoryStringFieldLeg(kind, field string, accepted bool) string {
	guard := `
    AND runtime_fact.fact_kind = '` + kind + `'`
	if accepted {
		guard += groupedInventoryAcceptedCorrelationSQL
	}
	guard += `
    AND jsonb_typeof(runtime_fact.payload->'` + field + `') = 'string'
    AND BTRIM(runtime_fact.payload->>'` + field + `') <> ''`
	return groupedInventoryMapLeg("BTRIM(runtime_fact.payload->>'"+field+"')", "", guard)
}

// groupedInventoryWorkloadEntityKeyLeg emits one map row per workload-prefixed
// entity key of a workload identity fact. The workload_id filter matches an
// identity fact whose entity_keys (array or bare string) holds the filter
// value or its BTRIM when the value starts with "workload:", so the bucket must
// too or bucket X would miss findings the filter returns.
var groupedInventoryWorkloadEntityKeyLeg = groupedInventoryMapLeg(
	"BTRIM(entity_key.value)",
	`
  CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE
      WHEN jsonb_typeof(runtime_fact.payload->'entity_keys') = 'array'
        THEN runtime_fact.payload->'entity_keys'
      WHEN jsonb_typeof(runtime_fact.payload->'entity_keys') = 'string'
        THEN jsonb_build_array(runtime_fact.payload->'entity_keys')
      ELSE '[]'::jsonb
    END
  ) AS entity_key(value)`,
	`
    AND runtime_fact.fact_kind = 'reducer_workload_identity'
    AND BTRIM(entity_key.value) LIKE 'workload:%'`,
)

// groupedInventoryMapRowsSQL selects the map legs of one dimension from the
// closed GroupedInventoryDimension set; request text never reaches the SQL.
func groupedInventoryMapRowsSQL(dimension GroupedInventoryDimension) (string, error) {
	switch dimension {
	case GroupedInventoryByService:
		return groupedInventoryStringFieldLeg(serviceCatalogCorrelationFactKind, "service_id", true), nil
	case GroupedInventoryByWorkload:
		return groupedInventoryStringFieldLeg("reducer_workload_identity", "workload_id", false) +
			"\n  UNION ALL\n" + groupedInventoryWorkloadEntityKeyLeg +
			"\n  UNION ALL\n" + groupedInventoryStringFieldLeg(serviceCatalogCorrelationFactKind, "workload_id", true), nil
	case GroupedInventoryByEnvironment:
		return groupedInventoryStringFieldLeg(cicdRunCorrelationFactKind, "environment", true), nil
	default:
		return "", fmt.Errorf("unsupported supply chain impact grouped inventory dimension: %q", dimension)
	}
}

// groupedInventoryTailSQL fans each finding out to every granted group its
// repository maps to (unmapped findings land in the "" unattributed bucket),
// aggregates per bucket, pages deterministically, and returns the whole-set
// finding count on every row. The totals row is the driving relation, so an
// empty page still returns one row carrying distinct_findings.
var groupedInventoryTailSQL = `,
grouped_map AS MATERIALIZED (
  SELECT map_row.repository_id,
         map_row.dim_id,
         -- COALESCE: an empty grant array may bind as NULL, and NULL must
         -- read as "not granted" here or a hidden repository never marks its
         -- bucket partial.
         BOOL_OR(COALESCE(` + groupedInventoryGrantSQL("map_row.repository_id", "map_row.scope_id") + `, FALSE)) AS granted
  FROM grouped_map_rows AS map_row
  WHERE COALESCE(map_row.repository_id, '') <> ''
  GROUP BY map_row.repository_id, map_row.dim_id
),
grouped_hidden AS MATERIALIZED (
  SELECT DISTINCT hidden_map.dim_id
  FROM grouped_map AS hidden_map
  WHERE NOT hidden_map.granted
),
grouped_cells AS MATERIALIZED (
  SELECT COALESCE(fan_map.dim_id, '') AS bucket,
         COALESCE(NULLIF(finding.impact_status, ''), 'unknown') AS impact_status,
         COALESCE(NULLIF(finding.severity_bucket, ''), 'none') AS severity_bucket,
         COUNT(*) AS cell_count,
         MAX(finding.priority_score) AS max_priority
  FROM grouped_findings AS finding
  LEFT JOIN grouped_map AS fan_map
    ON fan_map.repository_id = finding.repository_id
   AND fan_map.granted
  GROUP BY 1, 2, 3
),
grouped_page AS MATERIALIZED (
  SELECT bucket.bucket,
         bucket.bucket_count,
         bucket.max_priority
  FROM (
    SELECT cell.bucket,
           SUM(cell.cell_count)::bigint AS bucket_count,
           COALESCE(MAX(cell.max_priority), 0)::bigint AS max_priority
    FROM grouped_cells AS cell
    GROUP BY cell.bucket
  ) AS bucket
  ORDER BY bucket.max_priority DESC, bucket.bucket_count DESC, bucket.bucket ASC
  LIMIT $21 OFFSET $22
),
grouped_status AS (
  SELECT status_cell.bucket,
         jsonb_object_agg(status_cell.impact_status, status_cell.status_count) AS by_status
  FROM (
    SELECT cell.bucket, cell.impact_status, SUM(cell.cell_count)::bigint AS status_count
    FROM grouped_cells AS cell
    JOIN grouped_page AS page ON page.bucket = cell.bucket
    GROUP BY cell.bucket, cell.impact_status
  ) AS status_cell
  GROUP BY status_cell.bucket
),
grouped_severity AS (
  SELECT severity_cell.bucket,
         jsonb_object_agg(severity_cell.severity_bucket, severity_cell.severity_count) AS by_severity
  FROM (
    SELECT cell.bucket, cell.severity_bucket, SUM(cell.cell_count)::bigint AS severity_count
    FROM grouped_cells AS cell
    JOIN grouped_page AS page ON page.bucket = cell.bucket
    GROUP BY cell.bucket, cell.severity_bucket
  ) AS severity_cell
  GROUP BY severity_cell.bucket
),
grouped_total AS (
  SELECT COUNT(*)::bigint AS distinct_findings
  FROM grouped_findings
)
SELECT page.bucket,
       page.bucket = '' AS unattributed,
       page.bucket_count,
       page.max_priority,
       status.by_status,
       severity.by_severity,
       (hidden.dim_id IS NOT NULL) AS hidden_by_grant,
       total.distinct_findings
FROM grouped_total AS total
LEFT JOIN grouped_page AS page ON TRUE
LEFT JOIN grouped_status AS status ON status.bucket = page.bucket
LEFT JOIN grouped_severity AS severity ON severity.bucket = page.bucket
LEFT JOIN grouped_hidden AS hidden ON hidden.dim_id = page.bucket
ORDER BY page.max_priority DESC, page.bucket_count DESC, page.bucket ASC`

// groupedInventoryQuery builds the winners-sourced grouped inventory query for
// one dimension. The runtime-filter CTE is the same one the aggregate and
// winners list reads use, so composed filters (group_by=environment with
// service_id=X) keep their AND semantics; it is inert when the runtime filters
// are blank. The protected-reads marker is substituted closed (FALSE), as the
// aggregate composition does.
func groupedInventoryQuery(dimension GroupedInventoryDimension) (string, error) {
	mapRows, err := groupedInventoryMapRowsSQL(dimension)
	if err != nil {
		return "", err
	}
	query := `
WITH ` + supplyChainImpactRuntimeFilterCTE("$8", "$9", "$10", "$18", "$19") + `,` +
		groupedInventoryFindingsCTE + `,
grouped_map_rows AS MATERIALIZED (` + mapRows + `
)` + groupedInventoryTailSQL
	return supplyChainImpactReadModeSQL(query, false), nil
}
