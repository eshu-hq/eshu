// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
//
// The state side is ARN-first by construction (#7666). state_arn_matches
// probes fact_records_terraform_state_resource_arn_idx (migration 180) once
// per AWS ARN through an OFFSET 0 LATERAL, and the MATERIALIZED fence keeps
// the active-generation join to ingestion_scopes outside that loop. Both
// fences are load-bearing: the planner estimates aws_generation_arns at one
// row, and when it may reorder it ran the 342-scope active-state join once
// per ARN (129,618 index scans, 11.16M buffers, 36 s on the 1/20-scale shim;
// 14-20 minutes per run in production). Without the LATERAL fence a
// stale fact_kind estimate turns the probe into one full pass over the ARN
// index (278,044 buffers). The fenced form holds 1,625-2,668 buffers with
// fresh, absent, or stale table statistics, so it needs no expression
// statistics object.
// Evidence: docs/internal/evidence/7666-aws-drift-arn-index.md.
//
// The text from the start through the aws_generation_arns CTE is shared with
// the #7666 row differential's reference statement, which derives it from
// this constant at the "),\nstate_arn_matches" boundary.
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
),
state_arn_matches AS MATERIALIZED (
    SELECT
        aws_arn.arn         AS arn,
        state.fact_id       AS fact_id,
        state.scope_id      AS scope_id,
        state.generation_id AS generation_id,
        state.payload       AS payload
    FROM aws_generation_arns AS aws_arn
    CROSS JOIN LATERAL (
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.payload
        FROM fact_records AS fact
        WHERE fact.fact_kind = 'terraform_state_resource'
          AND btrim(COALESCE(fact.payload->'attributes'->>'arn', '')) <> ''
          AND fact.payload->'attributes'->>'arn' = aws_arn.arn
        OFFSET 0
    ) AS state
)
SELECT
    state.scope_id            AS state_scope_id,
    state.generation_id       AS state_generation_id,
    state.payload->>'address' AS address,
    state.payload             AS payload
FROM state_arn_matches AS state
JOIN ingestion_scopes AS scope
  ON scope.scope_id = state.scope_id
 AND scope.active_generation_id = state.generation_id
ORDER BY state.arn ASC, state.scope_id ASC, state.payload->>'address' ASC, state.fact_id ASC
`
