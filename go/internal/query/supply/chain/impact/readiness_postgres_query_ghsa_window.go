// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// readinessGHSARollingWindowScopesCTE builds the expected rows for rolling
// GHSA windows (eshu-hq/eshu#10464, design 10037 section 6): one eligible row
// per planned window scope in the latest run's requested_scope_set. It is
// modelled on readinessNVDWindowScopesCTE and spliced ahead of it, so the
// NVD guard test keeps its section boundaries.
//
// Two differences from the NVD leg follow from the GHSA collector contract.
// First, planned GHSA entries carry no endpoint candidates: the window is a
// recent-change feed over one advisory type, never a multi-endpoint read, so
// there is no descriptor to match; the planned modified end rides a text
// column instead. Second, source states are keyed by stream, the anchor
// scope, not by window (two rows per instance, never one per window), so the
// state join maps each planned window scope back to its stream scope; the
// window bounds ride the stream row's window columns.
//
// Coverage is eligible: a planned rolling window is expected collection. The
// committed match is the exact scope with covered_through equal to the
// planned window end: only a complete, warning-free, fresh snapshot stating
// that far a coverage proves the window ran, and a missing one reads as
// ghsa:committed_snapshot_missing. covered_through compares as timestamptz
// (never as text) so equivalent RFC 3339 spellings still match; both sides
// carry the strict shape guard, so a malformed snapshot or planned entry
// skips its row instead of aborting the read. It adds no params.
// readinessGHSACandidateEndValidSQL guards a planned GHSA window end the
// state-candidate leg carries as a text column: the predicate raises no
// error for any input, and every cast of the column sits in the THEN arm of
// a CASE WHEN on it.
const readinessGHSACandidateEndValidSQL = `(TRIM(target.modified_end) ~ '` + readinessRFC3339Pattern + `'
        AND pg_input_is_valid(TRIM(target.modified_end), 'timestamp with time zone'))`

const readinessGHSARollingWindowScopesCTE = `ghsa_rolling_window_raw AS (
    SELECT run.collector_instance_id, element AS target
    FROM latest_vulnerability_planner_runs AS run
    CROSS JOIN LATERAL JSONB_ARRAY_ELEMENTS(
        CASE WHEN JSONB_TYPEOF(run.requested_scope_set->'targets') = 'array'
            THEN run.requested_scope_set->'targets' ELSE '[]'::jsonb END
    ) AS element
    WHERE TRIM(element->>'source') = 'ghsa'
      AND (NULLIF(TRIM(element->>'scope_id'), '') LIKE 'vuln-intel://ghsa/reviewed/\_window/%'
        OR NULLIF(TRIM(element->>'scope_id'), '') LIKE 'vuln-intel://ghsa/malware/\_window/%')
      AND NULLIF(TRIM(element->>'modified_start'), '') IS NOT NULL
      AND NULLIF(TRIM(element->>'modified_end'), '') IS NOT NULL
),
ghsa_rolling_window_scopes AS (
    SELECT collector_instance_id,
        NULLIF(TRIM(target->>'scope_id'), '') AS scope_id,
        'ghsa'::text AS source, ''::text AS ecosystem,
        TRUE AS coverage_eligible,
        NULLIF(TRIM(target->>'modified_end'), '') AS modified_end
    FROM ghsa_rolling_window_raw
    WHERE ` + readinessTargetModifiedStartValidSQL + `
      AND ` + readinessTargetModifiedEndValidSQL + `
    GROUP BY collector_instance_id, scope_id, modified_end
),
ghsa_rolling_window_state_candidates AS (
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
        SELECT * FROM ghsa_rolling_window_scopes
        ORDER BY collector_instance_id, scope_id, source, ecosystem
        LIMIT 200
    ) AS target
    -- The GHSA state row lives under the stream (anchor) scope, so the join
    -- strips the /_window/<start>-<end> suffix back to the stream. Only an
    -- active, complete snapshot for this exact executed window proves the
    -- target ran (covered). covered_through must equal the planned window
    -- end: the host refuses to arm the cursor on anything else.
    LEFT JOIN LATERAL (
        SELECT * FROM vulnerability_source_states AS state
        WHERE state.collector_instance_id = target.collector_instance_id
          AND state.scope_id = regexp_replace(target.scope_id, '/_window/[0-9]+-[0-9]+$', '')
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
              AND snapshot.payload->>'source' = 'ghsa'
              AND snapshot.payload @> '{"complete": true}'::jsonb
              AND NULLIF(TRIM(snapshot.payload->>'warning_code'), '') IS NULL
              AND NULLIF(TRIM(snapshot.payload->>'warning_message'), '') IS NULL
              AND JSONB_TYPEOF(snapshot.payload->'result_count') = 'number'
              AND snapshot.payload->>'result_count' ~ '^[0-9]+$'
              AND COALESCE(NULLIF(TRIM(snapshot.payload->>'cache_freshness', ''), ''), 'fresh') = 'fresh'
              AND CASE WHEN JSONB_TYPEOF(snapshot.payload->'covered_through') = 'string'
                  AND TRIM(snapshot.payload->>'covered_through') ~ '` + readinessRFC3339Pattern + `'
                  AND pg_input_is_valid(TRIM(snapshot.payload->>'covered_through'), 'timestamp with time zone')
                  THEN TRIM(snapshot.payload->>'covered_through')::timestamptz ELSE NULL END
                = CASE WHEN ` + readinessGHSACandidateEndValidSQL + `
                  THEN TRIM(target.modified_end)::timestamptz ELSE NULL END
              AND ` + readinessGHSACandidateEndValidSQL + `
            OFFSET 0
        ) AS snapshot
    ) AS committed
),
`
