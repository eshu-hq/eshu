package postgres

// listAWSCloudRuntimeResourcesForGenerationQuery returns AWS resource rows for
// one sealed AWS collector generation. The ARN predicate is load-bearing: the
// runtime drift classifier is ARN-keyed, so blank ARN rows cannot safely join
// against Terraform state.
const listAWSCloudRuntimeResourcesForGenerationQuery = `
SELECT
    fact.payload->>'arn' AS arn,
    fact.payload         AS payload
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'aws_resource'
  AND btrim(COALESCE(fact.payload->>'arn', '')) <> ''
ORDER BY fact.payload->>'arn' ASC, fact.fact_id ASC
`

// listActiveStateResourcesForAWSARNsQuery returns active
// terraform_state_resource facts whose attributes.arn matches the caller's
// current AWS generation. The JSON allowlist in $3 keeps the state scan tied to
// the AWS ARNs already loaded in memory, while the aws_generation_arns CTE
// rechecks scope_id + generation_id at the database boundary so stale caller
// input cannot widen the join.
const listActiveStateResourcesForAWSARNsQuery = `
WITH requested_arns AS (
    SELECT DISTINCT value AS arn
    FROM jsonb_array_elements_text($3::jsonb) AS value
    WHERE btrim(value) <> ''
),
aws_generation_arns AS (
    SELECT DISTINCT fact.payload->>'arn' AS arn
    FROM fact_records AS fact
    JOIN requested_arns AS requested
      ON requested.arn = fact.payload->>'arn'
    WHERE fact.scope_id = $1
      AND fact.generation_id = $2
      AND fact.fact_kind = 'aws_resource'
      AND btrim(COALESCE(fact.payload->>'arn', '')) <> ''
)
SELECT
    fact.scope_id           AS state_scope_id,
    fact.generation_id      AS state_generation_id,
    fact.payload->>'address' AS address,
    fact.payload            AS payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN aws_generation_arns AS aws_arn
  ON aws_arn.arn = fact.payload->'attributes'->>'arn'
WHERE fact.fact_kind = 'terraform_state_resource'
  AND btrim(COALESCE(fact.payload->'attributes'->>'arn', '')) <> ''
ORDER BY aws_arn.arn ASC, fact.scope_id ASC, fact.payload->>'address' ASC, fact.fact_id ASC
`
