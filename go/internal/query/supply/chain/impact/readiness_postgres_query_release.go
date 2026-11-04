// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// readinessDerivedReleaseMatchCTE selects the derived source states that decide
// coverage for each requested package. One package can have several derived
// scopes that are all current: an Alpine package gets one scope per release
// (#9993), and every one of them must be fresh before the package is covered
// (#10015).
//
// Coverage is decided per release. Inside one release the newest state wins,
// because superseded versions stay in the state table forever: a fix that
// demanded every row be fresh would read stale for good. A scope with no
// release qualifier shares one group per package, so every other ecosystem and
// distro keeps its one newest-wins scope. Release and distro come from the
// scope id, and only label and group rows: the package-key join stays the only
// way a state reaches a package.
//
// The latest planner run supplies what the state tables cannot. A state row and
// its key rows are written together, so a scope the collector has not reached
// leaves no row at all, and only the plan shows it is expected. Each planned
// release of a requested package becomes a pending row, which any real state
// for that release outranks (NULLS LAST). The plan also retires state: once it
// names releases for a package and distro, a release it dropped and a legacy
// scope with no release stop counting, or they would read stale forever. When
// the plan names no release for that package and distro, every release row
// stands, so a missing or older plan fails toward not covered.
//
// Bounds: the plan is capped by the derivation budget (5000 queries), and the
// EXISTS gate skips it for requests with no OS package, the only ecosystem with
// release scopes. MATERIALIZED on the requested names keeps the plan expansion
// from rerunning per configured ecosystem, and the name join runs above the
// expansion so it hashes once rather than per target. The release parse uses
// SPLIT_PART rather than a regular expression and waits until after that join,
// because it runs once per matched row. The sorts compare under COLLATE "C"
// because deterministic collations agree on equality and byte comparison is far
// cheaper than a locale's.
const readinessDerivedReleaseMatchCTE = `derived_vulnerability_planned_release_targets AS MATERIALIZED (
    SELECT run.collector_instance_id, element.target
    FROM latest_vulnerability_planner_runs AS run
    CROSS JOIN LATERAL JSONB_ARRAY_ELEMENTS(
        CASE WHEN JSONB_TYPEOF(run.requested_scope_set->'targets') = 'array'
            THEN run.requested_scope_set->'targets' ELSE '[]'::jsonb END
    ) AS element(target)
    WHERE EXISTS (SELECT 1 FROM derived_request_packages AS request WHERE request.ecosystem = 'os')
      AND element.target->>'source' = 'osv'
      AND element.target->>'scope_id' LIKE '%release=%'
),
derived_vulnerability_planned_requested AS MATERIALIZED (
    SELECT planned.collector_instance_id, request.ecosystem, request.package_id,
        TRIM(planned.target->>'scope_id') AS scope_id,
        LOWER(TRIM(planned.target->>'ecosystem') COLLATE "C") COLLATE "default" AS distro,
        NULLIF(SPLIT_PART(SPLIT_PART(SPLIT_PART(REPLACE(planned.target->>'scope_id', '?', '&'), '&release=', 2), '&', 1), '#', 1), '') AS release
    FROM (
        SELECT target.collector_instance_id, target.target,
            LOWER(TRIM(raw.package_name) COLLATE "C") COLLATE "default" AS package_name
        FROM derived_vulnerability_planned_release_targets AS target
        CROSS JOIN LATERAL (
            SELECT target.target->>'package_name' AS package_name
            UNION ALL
            SELECT batch.entry->>'package_name'
            FROM JSONB_ARRAY_ELEMENTS(CASE WHEN JSONB_TYPEOF(target.target->'queries') = 'array'
                THEN target.target->'queries' ELSE '[]'::jsonb END) AS batch(entry)
        ) AS raw
        OFFSET 0
    ) AS planned
    JOIN derived_request_keys AS request ON request.package_name = planned.package_name
),
derived_vulnerability_planned_releases AS (
    SELECT DISTINCT ON (planned.collector_instance_id COLLATE "C", planned.ecosystem COLLATE "C", planned.package_id COLLATE "C",
            planned.distro COLLATE "C", planned.release COLLATE "C")
        planned.collector_instance_id, planned.ecosystem, planned.package_id,
        planned.distro, planned.release, planned.scope_id
    FROM derived_vulnerability_planned_requested AS planned
    JOIN derived_vulnerability_source_config AS config
      ON config.collector_instance_id = planned.collector_instance_id
     AND config.source = 'osv'
     AND config.ecosystem = planned.ecosystem
     AND config.derivation_ecosystem = planned.distro
    WHERE planned.release IS NOT NULL
    ORDER BY planned.collector_instance_id COLLATE "C", planned.ecosystem COLLATE "C", planned.package_id COLLATE "C",
        planned.distro COLLATE "C", planned.release COLLATE "C", planned.scope_id COLLATE "C"
),
derived_vulnerability_source_hits AS (
    SELECT request.ecosystem, request.package_id, hit.*,
        CASE WHEN hit.scope_id LIKE 'vuln-intel://osv/%' THEN SPLIT_PART(hit.scope_id, '/', 4) END AS distro,
        CASE WHEN hit.scope_id LIKE '%release=%' THEN NULLIF(SPLIT_PART(SPLIT_PART(SPLIT_PART(REPLACE(hit.scope_id, '?', '&'), '&release=', 2), '&', 1), '#', 1), '') END AS release
    FROM derived_request_packages AS request
    CROSS JOIN LATERAL (
        SELECT state.collector_instance_id, state.scope_id, state.source, state.updated_at,
            state.freshness_state, state.terminal_status, state.last_error_class, state.last_success_at,
            state.last_attempt_at, state.next_retry_at, state.result_count, state.warning_count
        FROM (
            SELECT package.collector_instance_id, package.scope_id
            FROM vulnerability_source_state_packages AS package
            WHERE package.ecosystem = request.ecosystem
              AND package.package_name = ANY(request.package_names)
            OFFSET 0
        ) AS package
        CROSS JOIN LATERAL (
            SELECT state.* FROM vulnerability_source_states AS state
            WHERE state.collector_instance_id = package.collector_instance_id
              AND state.scope_id = package.scope_id
            OFFSET 0
        ) AS state
        OFFSET 0
    ) AS hit
    JOIN derived_vulnerability_source_config AS config
      ON config.collector_instance_id = hit.collector_instance_id
     AND config.source = hit.source
     AND config.ecosystem = request.ecosystem
),
derived_vulnerability_source_matched AS (
    SELECT DISTINCT ON (ranked.collector_instance_id COLLATE "C", ranked.ecosystem COLLATE "C", ranked.package_id COLLATE "C",
            ranked.source COLLATE "C", CASE WHEN ranked.release IS NULL THEN NULL ELSE ranked.distro END COLLATE "C", ranked.release COLLATE "C")
        ranked.ecosystem, ranked.package_id, ranked.collector_instance_id, ranked.scope_id, ranked.source,
        ranked.updated_at, ranked.freshness_state, ranked.terminal_status, ranked.last_error_class,
        ranked.last_success_at, ranked.last_attempt_at, ranked.next_retry_at,
        ranked.result_count, ranked.warning_count, ranked.distro, ranked.release
    FROM (
        SELECT candidate.*,
            BOOL_OR(candidate.planned) OVER (PARTITION BY candidate.collector_instance_id COLLATE "C", candidate.ecosystem COLLATE "C",
                candidate.package_id COLLATE "C", candidate.distro COLLATE "C") AS distro_planned,
            BOOL_OR(candidate.planned) OVER (PARTITION BY candidate.collector_instance_id COLLATE "C", candidate.ecosystem COLLATE "C",
                candidate.package_id COLLATE "C", candidate.distro COLLATE "C", candidate.release COLLATE "C") AS release_planned
        FROM (
            SELECT hit.*, FALSE AS planned FROM derived_vulnerability_source_hits AS hit
            UNION ALL
            SELECT planned.ecosystem, planned.package_id, planned.collector_instance_id, planned.scope_id,
                'osv'::text, NULL::timestamptz, 'pending'::text, 'pending'::text, NULL::text,
                NULL::timestamptz, NULL::timestamptz, NULL::timestamptz, 0, 0,
                planned.distro, planned.release, TRUE
            FROM derived_vulnerability_planned_releases AS planned
        ) AS candidate
    ) AS ranked
    WHERE NOT ranked.distro_planned OR ranked.release_planned
    ORDER BY ranked.collector_instance_id COLLATE "C", ranked.ecosystem COLLATE "C", ranked.package_id COLLATE "C",
        ranked.source COLLATE "C", CASE WHEN ranked.release IS NULL THEN NULL ELSE ranked.distro END COLLATE "C", ranked.release COLLATE "C",
        ranked.updated_at DESC NULLS LAST, ranked.scope_id
),
`
