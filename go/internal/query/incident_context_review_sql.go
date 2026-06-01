package query

const listIncidentPullRequestsByCommitQuery = `
SELECT
    trigger_id,
    provider,
    repository_full_name,
    target_sha,
    pull_request_number,
    pull_request_url,
    pull_request_title
FROM webhook_refresh_triggers
WHERE provider = 'github'
  AND event_kind = 'pull_request_merged'
  AND decision = 'accepted'
  AND target_sha = $1
  AND pull_request_url <> ''
ORDER BY received_at ASC, trigger_id ASC
LIMIT $2
`

const listIncidentWorkItemExternalLinksByURLQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'work_item.external_link'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'url' = $1
ORDER BY fact.fact_id ASC
LIMIT $2
`

const listIncidentWorkItemRecordsByKeyQuery = incidentContextFactSelect + `
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'work_item.record'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'work_item_key' = $1
ORDER BY fact.fact_id ASC
LIMIT $2
`
