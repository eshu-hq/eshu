// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// readinessNVDWindowScopesCTE builds the expected rows for rolling NVD
// windows (eshu-hq/eshu#9971): one eligible row per planned window scope in
// the latest run's requested_scope_set. The bounds ride the coordinator's
// planned entry (never the scope id), parsed with the same guards as
// configured windows, so a malformed entry skips its row instead of
// aborting the read. Coverage is eligible: a planned rolling window is
// expected collection. The committed match is the same descriptor equality
// the configured path uses, so a committed window reads as covered and a
// missing one reads as nvd:committed_snapshot_missing. It adds no params.
const readinessNVDWindowScopesCTE = `nvd_rolling_window_raw AS (
    SELECT run.collector_instance_id, element AS target
    FROM latest_vulnerability_planner_runs AS run
    CROSS JOIN LATERAL JSONB_ARRAY_ELEMENTS(
        CASE WHEN JSONB_TYPEOF(run.requested_scope_set->'targets') = 'array'
            THEN run.requested_scope_set->'targets' ELSE '[]'::jsonb END
    ) AS element
    WHERE TRIM(element->>'source') = 'nvd'
      AND NULLIF(TRIM(element->>'scope_id'), '') LIKE 'vuln-intel://nvd/\_window/%'
      AND NULLIF(TRIM(element->>'modified_start'), '') IS NOT NULL
      AND NULLIF(TRIM(element->>'modified_end'), '') IS NOT NULL
      AND JSONB_TYPEOF(element->'endpoint_candidates') = 'array'
),
nvd_rolling_window_expected AS (
    SELECT collector_instance_id,
        NULLIF(TRIM(target->>'scope_id'), '') AS scope_id,
        JSONB_BUILD_OBJECT(
            'version', 'nvd.v1', 'source', 'nvd',
            'mode', 'window',
            'cve_ids', '[]'::jsonb,
            'modified_start_unix_us', CASE WHEN ` + readinessTargetModifiedStartValidSQL + `
                THEN (EXTRACT(EPOCH FROM TRIM(target->>'modified_start')::timestamptz) * 1000000)::bigint ELSE 0 END,
            'modified_end_unix_us', CASE WHEN ` + readinessTargetModifiedEndValidSQL + `
                THEN (EXTRACT(EPOCH FROM TRIM(target->>'modified_end')::timestamptz) * 1000000)::bigint ELSE 0 END,
            'results_per_page', CASE WHEN NULLIF(TRIM(target->>'modified_start'), '') IS NULL AND
                NULLIF(TRIM(target->>'modified_end'), '') IS NULL THEN 0
                WHEN ` + readinessTargetResultsPerPageValidSQL + ` THEN (target->'results_per_page')::numeric::int
                ELSE 0 END,
            'endpoint_candidates', target->'endpoint_candidates'
        ) AS expected_descriptor
    FROM nvd_rolling_window_raw
    WHERE ` + readinessTargetModifiedStartValidSQL + `
      AND ` + readinessTargetModifiedEndValidSQL + `
      AND EXISTS (
          SELECT 1 FROM JSONB_ARRAY_ELEMENTS_TEXT(target->'endpoint_candidates') AS endpoint(candidate)
          WHERE NULLIF(TRIM(endpoint.candidate), '') IS NOT NULL
      )
),
nvd_rolling_window_scopes AS (
    SELECT collector_instance_id, scope_id, 'nvd'::text AS source, ''::text AS ecosystem,
        TRUE AS coverage_eligible,
        MIN(expected_descriptor::text)::jsonb AS expected_descriptor
    FROM nvd_rolling_window_expected
    GROUP BY collector_instance_id, scope_id
),
nvd_rolling_window_state_candidates AS (
    SELECT
        target.collector_instance_id,
        target.scope_id,
        target.source,
        target.ecosystem,
        state.window_start,
        state.window_end,
        state.last_attempt_at,
        state.last_success_at,
        state.next_retry_at,
        state.last_error_class,
        COALESCE(state.freshness_state, 'pending') AS freshness_state,
        COALESCE(state.terminal_status, 'pending') AS terminal_status,
        COALESCE(state.result_count, 0) AS result_count,
        COALESCE(state.warning_count, 0) AS warning_count,
        state.updated_at,
        target.coverage_eligible AND committed.covered AS coverage_eligible,
        target.coverage_eligible AND committed.zero_result AS coverage_zero,
        target.coverage_eligible AND NOT committed.covered AS coverage_missing,
        FALSE AS multi_cve_target,
        NULL::jsonb AS derived_detail
    FROM (
        -- Same 200-candidate bound as the configured leg, ahead of the
        -- keyed state and snapshot probes; the overflow CTE counts these
        -- scopes so a truncated read still reports incomplete.
        SELECT * FROM nvd_rolling_window_scopes
        ORDER BY collector_instance_id, scope_id, source, ecosystem
        LIMIT 200
    ) AS target
    -- A source-state row is written before the generation commit. Only an
    -- active, complete snapshot for this exact executed window proves the
    -- target ran (covered). The match is the configured shape unchanged.
    LEFT JOIN LATERAL (
        SELECT * FROM vulnerability_source_states AS state
        WHERE state.collector_instance_id = target.collector_instance_id
          AND state.scope_id = target.scope_id
          AND state.source = target.source
        OFFSET 0
    ) AS state ON TRUE
    CROSS JOIN LATERAL (
        SELECT COUNT(*) > 0 AS covered,
            COALESCE(BOOL_AND(snapshot.payload->'result_count' = '0'::jsonb), FALSE) AS zero_result
        FROM (
            SELECT snapshot.payload FROM vulnerability_source_snapshot_active AS snapshot
            WHERE snapshot.scope_id = target.scope_id
              AND snapshot.payload->>'collector_instance_id' = target.collector_instance_id
              AND snapshot.payload->>'source' = 'nvd'
              AND snapshot.payload @> '{"complete": true}'::jsonb
              AND NULLIF(TRIM(snapshot.payload->>'warning_code'), '') IS NULL
              AND NULLIF(TRIM(snapshot.payload->>'warning_message'), '') IS NULL
              AND JSONB_TYPEOF(snapshot.payload->'result_count') = 'number'
              AND snapshot.payload->>'result_count' ~ '^[0-9]+$'
              AND COALESCE(NULLIF(TRIM(snapshot.payload->>'cache_freshness', ''), ''), 'fresh') = 'fresh'
              AND CASE WHEN JSONB_TYPEOF(snapshot.payload->'coverage_descriptor') = 'object'
                  THEN (snapshot.payload->'coverage_descriptor') - 'executed_endpoint'::text = target.expected_descriptor
                   AND (target.expected_descriptor->'endpoint_candidates') ?
                       (snapshot.payload->'coverage_descriptor'->>'executed_endpoint')
                  ELSE FALSE END
            OFFSET 0
        ) AS snapshot
    ) AS committed
),
`
