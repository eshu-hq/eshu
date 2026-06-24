// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const incidentContextFactSelect = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.source_confidence,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.payload
`

const listIncidentContextIncidentsQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.source_system = $1
  AND (
      fact.payload->>'provider_incident_id' = $2
      OR (
          NULLIF(fact.payload->>'provider_incident_id', '') IS NULL
          AND fact.source_record_id = $2
      )
  )
  AND ($3 = '' OR fact.scope_id = $3)
  AND fact.fact_kind = 'incident.record'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
ORDER BY fact.scope_id ASC, fact.observed_at DESC, fact.fact_id ASC
LIMIT $4
`

const listIncidentContextTimelineQuery = incidentContextFactSelect + `
FROM fact_records AS fact
WHERE fact.payload->>'provider_incident_id' = $1
  AND fact.scope_id = $2
  AND fact.generation_id = $3
  AND fact.fact_kind = 'incident.lifecycle_event'
  AND fact.is_tombstone = FALSE
ORDER BY fact.payload->>'created_at' ASC, fact.fact_id ASC
LIMIT $4
`

const listIncidentContextChangeCandidatesQuery = incidentContextFactSelect + `
FROM fact_records AS fact
WHERE fact.payload @> jsonb_build_object('services', jsonb_build_array(jsonb_build_object('id', $1::text)))
  AND fact.scope_id = $2
  AND fact.generation_id = $3
  AND ($4::timestamptz IS NULL OR NULLIF(fact.payload->>'timestamp', '')::timestamptz >= $4::timestamptz)
  AND ($5::timestamptz IS NULL OR NULLIF(fact.payload->>'timestamp', '')::timestamptz <= $5::timestamptz)
  AND fact.fact_kind = 'change.record'
  AND fact.is_tombstone = FALSE
ORDER BY fact.payload->>'timestamp' ASC, fact.fact_id ASC
LIMIT $6
`
