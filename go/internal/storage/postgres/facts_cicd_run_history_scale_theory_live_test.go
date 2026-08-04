// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const cicdRunHistoryScopeRankTheoryQuery = `
WITH requested_run_keys AS MATERIALIZED (
    SELECT
        'github_actions'::text AS provider,
        FORMAT('run-%s', run_number)::text AS run_id,
        '1'::text AS run_attempt
    FROM GENERATE_SERIES(1, 90) AS run_number
),
ranked_run_facts AS MATERIALIZED (
    SELECT
        fact.fact_kind,
        fact.stable_fact_key,
        fact.is_tombstone,
        fact.payload,
        ROW_NUMBER() OVER (
            PARTITION BY fact.fact_kind, fact.stable_fact_key
            ORDER BY generation.ingested_at DESC,
                     generation.generation_id DESC,
                     fact.observed_at DESC,
                     fact.fact_id DESC
        ) AS fact_rank
    FROM fact_records AS fact
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.scope_id = 'scope-ci-scale'
      AND fact.fact_kind = ANY(ARRAY[
          'ci.run',
          'ci.artifact',
          'ci.environment_observation',
          'ci.trigger_edge',
          'ci.step'
      ]::text[])
      AND generation.status IN ('active', 'completed', 'superseded')
      AND (generation.ingested_at, generation.generation_id)
          < ('2026-08-04T13:00:25Z'::timestamptz, 'perf-gen-25')
)
SELECT COUNT(*)
FROM ranked_run_facts AS fact
WHERE fact.fact_rank = 1
  AND fact.is_tombstone = FALSE
  AND EXISTS (
      SELECT 1
      FROM requested_run_keys AS requested
      WHERE BTRIM(fact.payload->>'provider') = requested.provider
        AND BTRIM(fact.payload->>'run_id') = requested.run_id
        AND COALESCE(NULLIF(BTRIM(fact.payload->>'run_attempt'), ''), '1') = requested.run_attempt
  )`
