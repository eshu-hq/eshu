// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const listIncidentServiceCatalogOperationalLinksQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'service_catalog.operational_link'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'url' = $1
ORDER BY fact.fact_id ASC
LIMIT $2
`

const listIncidentKubernetesCorrelationsByImageQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_kubernetes_correlation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    ($1 <> '' AND fact.payload->>'source_digest' = $1)
    OR ($2 <> '' AND fact.payload->>'image_ref' = $2)
  )
  AND fact.payload->>'outcome' IN ('exact', 'derived', 'ambiguous')
ORDER BY fact.fact_id ASC
LIMIT $3
`

const listIncidentCICDRunCorrelationsByImageRefQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_ci_cd_run_correlation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'image_ref' = $1
  AND fact.payload->>'outcome' IN ('exact', 'derived', 'ambiguous')
ORDER BY fact.fact_id ASC
LIMIT $2
`
