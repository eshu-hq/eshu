// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const listIncidentDeclaredPagerDutyRoutingQuery = `
SELECT
    entity_id,
    repo_id,
    relative_path,
    entity_name,
    start_line,
    end_line,
    metadata
FROM content_entities
WHERE entity_type = 'PagerDutyDeclaration'
  AND metadata->>'source_class' = 'declared'
  AND lower(coalesce(metadata->>'service_name', '')) = lower($1)
ORDER BY repo_id ASC, relative_path ASC, start_line ASC, entity_id ASC
LIMIT $2
`

const listIncidentAppliedPagerDutyRoutingQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'incident_routing.applied_pagerduty_resource'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'resource_class' = 'service'
  AND (
    ($1 <> '' AND fact.payload->>'provider_object_id' = $1)
    OR ($2 <> '' AND fact.payload->>'name_fingerprint' = $2)
  )
ORDER BY fact.scope_id ASC, fact.observed_at DESC, fact.fact_id ASC
LIMIT $3
`

const listIncidentObservedPagerDutyRoutingQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'incident_routing.observed_pagerduty_service'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    ($1 <> '' AND fact.payload->>'service_id' = $1)
    OR ($1 <> '' AND fact.payload->>'provider_object_id' = $1)
    OR ($2 <> '' AND fact.payload->>'name_fingerprint' = $2)
  )
ORDER BY fact.scope_id ASC, fact.observed_at DESC, fact.fact_id ASC
LIMIT $3
`

const listIncidentRoutingCoverageWarningsQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'incident_routing.coverage_warning'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.scope_id = $1
  AND (
    ($2 <> '' AND fact.payload->>'provider_object_id' = $2)
    OR ($2 <> '' AND fact.payload->>'service_id' = $2)
    OR (
      coalesce(fact.payload->>'provider_object_id', '') = ''
      AND coalesce(fact.payload->>'service_id', '') = ''
      AND fact.payload->>'resource_class' IN ('service', 'unknown')
      AND lower(coalesce(fact.payload->>'reason', '')) LIKE '%permission%'
    )
  )
ORDER BY fact.scope_id ASC, fact.observed_at DESC, fact.fact_id ASC
LIMIT $3
`
