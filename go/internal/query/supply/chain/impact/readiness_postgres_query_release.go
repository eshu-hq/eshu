// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// osPackageFleetReleaseLineExpression is the SQL mirror of the coordinator's
// Alpine release rule (installedAlpineOSVRelease, eshu-hq/eshu#9993): it reads
// OSV's Alpine release line ("v3.18") from an installed row's distro_version
// ("3.18.4") by dropping the patch level, and reports NULL for a version with
// fewer than two numeric dot fields or a non-numeric major or minor. A NULL
// line never equals a release, so an unreadable row neither proves nor blocks
// fleet presence for one. Payload names the jsonb payload column, with or
// without a table alias. TRIM strips spaces only where Go's TrimSpace strips
// every unicode space; a distro_version with exotic padding around a readable
// major.minor is the one shape the mirror misreads, and /etc/os-release never
// writes it. Migrations 281 and 282 build expression indexes on exactly this
// text (TestReadinessFleetReleaseLineExpressionMatchesMigration pins the
// three together): an index on any other spelling would not serve the probe.
func osPackageFleetReleaseLineExpression(payloadColumn string) string {
	version := "SPLIT_PART(TRIM(" + payloadColumn + "->>'distro_version'), ' ', 1)"
	major := "SPLIT_PART(" + version + ", '.', 1)"
	minor := "SPLIT_PART(" + version + ", '.', 2)"
	return "(CASE WHEN " + version + " LIKE '%.%'" +
		" AND " + major + " ~ '^[0-9]+$'" +
		" AND " + minor + " ~ '^[0-9]+$'" +
		" THEN 'v' || " + major + " || '.' || " + minor +
		" ELSE NULL END)"
}

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
// The latest planner run supplies one thing the state tables cannot. A state row
// and its key rows are written together, so a scope the collector has not
// reached leaves no row at all, and only the plan shows it is expected. Each
// planned release of a requested package becomes a pending row, which any real
// state for that release outranks (NULLS LAST). The plan never removes a state
// row. The coordinator plans one limit-sized window of the installed fleet per
// tick, so a plan that does not name a release says nothing about whether the
// release is still installed, and a rule that read absence as departure made the
// same state rows read fresh in one window and failed in the next.
//
// One rule reads state only. A scope with no release parameter at all is a legacy
// scope: the planner never mints one for an OS package once releases are read, and
// OSV answers a bare Alpine query with no advisories, so it never covered a
// release. It stops counting for a package and distro once a real state row with
// a release exists for the same instance, package and distro, or it would read
// stale for good. A pending row does not trigger it, and neither does a scope
// whose first release value is empty: that row has a release parameter, so it
// stands and fails toward not covered.
//
// A non-fresh release that left the fleet retires the same way, decided from
// the fleet and never from the plan (#10135): the batched EXISTS probe in
// readinessDerivedReleaseFleetProbeCTE checks each resolved Alpine (package,
// release) triple against the active os_package facts, and a triple neither
// name arm carries reads fresh. The retired release is named in the
// aggregate's retired_releases detail but never in incomplete_reasons, so it
// stops holding the package not covered without looking covered.
//
// A planned target's scope id is as wide as its batch (about 1.7 KB for 66
// packages), so the plan side carries the target's ordinal and looks the id up
// after the newest state per release is chosen. Carrying the id through the
// planned-release and window sorts spilled to disk at the 5000-query ceiling
// where the statement before this change did not. Each planned batch target
// must name a single release, because every package in its queries[] is
// attributed to the target's release.
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
    SELECT run.collector_instance_id, element.ord, element.target
    FROM latest_vulnerability_planner_runs AS run
    CROSS JOIN LATERAL JSONB_ARRAY_ELEMENTS(
        CASE WHEN JSONB_TYPEOF(run.requested_scope_set->'targets') = 'array'
            THEN run.requested_scope_set->'targets' ELSE '[]'::jsonb END
    ) WITH ORDINALITY AS element(target, ord)
    WHERE EXISTS (SELECT 1 FROM derived_request_packages AS request WHERE request.ecosystem = 'os')
      AND element.target->>'source' = 'osv'
      AND element.target->>'scope_id' LIKE '%release=%'
),
derived_vulnerability_planned_requested AS MATERIALIZED (
    SELECT planned.collector_instance_id, request.ecosystem, request.package_id, planned.ord,
        LOWER(TRIM(planned.target->>'ecosystem') COLLATE "C") COLLATE "default" AS distro,
        NULLIF(SPLIT_PART(SPLIT_PART(SPLIT_PART(REPLACE(planned.target->>'scope_id', '?', '&'), '&release=', 2), '&', 1), '#', 1), '') AS release
    FROM (
        SELECT target.collector_instance_id, target.ord, target.target,
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
        planned.distro, planned.release, planned.ord
    FROM derived_vulnerability_planned_requested AS planned
    JOIN derived_vulnerability_source_config AS config
      ON config.collector_instance_id = planned.collector_instance_id
     AND config.source = 'osv'
     AND config.ecosystem = planned.ecosystem
     AND config.derivation_ecosystem = planned.distro
    WHERE planned.release IS NOT NULL
    ORDER BY planned.collector_instance_id COLLATE "C", planned.ecosystem COLLATE "C", planned.package_id COLLATE "C",
        planned.distro COLLATE "C", planned.release COLLATE "C", planned.ord
),
derived_vulnerability_planned_scopes AS (
    SELECT target.collector_instance_id, target.ord, TRIM(target.target->>'scope_id') AS scope_id
    FROM derived_vulnerability_planned_release_targets AS target
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
derived_vulnerability_source_chosen AS (
    SELECT DISTINCT ON (ranked.collector_instance_id COLLATE "C", ranked.ecosystem COLLATE "C", ranked.package_id COLLATE "C",
            ranked.source COLLATE "C", CASE WHEN ranked.release IS NULL THEN NULL ELSE ranked.distro END COLLATE "C", ranked.release COLLATE "C")
        ranked.ecosystem, ranked.package_id, ranked.collector_instance_id, ranked.scope_id, ranked.source,
        ranked.updated_at, ranked.freshness_state, ranked.terminal_status, ranked.last_error_class,
        ranked.last_success_at, ranked.last_attempt_at, ranked.next_retry_at,
        ranked.result_count, ranked.warning_count, ranked.distro, ranked.release, ranked.planned_ord
    FROM (
        SELECT candidate.*,
            (candidate.release IS NULL AND candidate.scope_id NOT LIKE '%release=%') AS legacy,
            BOOL_OR(NOT candidate.planned AND candidate.release IS NOT NULL) OVER (PARTITION BY candidate.collector_instance_id COLLATE "C",
                candidate.ecosystem COLLATE "C", candidate.package_id COLLATE "C", candidate.distro COLLATE "C") AS distro_has_release_state
        FROM (
            SELECT hit.*, FALSE AS planned, NULL::bigint AS planned_ord FROM derived_vulnerability_source_hits AS hit
            UNION ALL
            SELECT planned.ecosystem, planned.package_id, planned.collector_instance_id, NULL::text,
                'osv'::text, NULL::timestamptz, 'pending'::text, 'pending'::text, NULL::text,
                NULL::timestamptz, NULL::timestamptz, NULL::timestamptz, 0, 0,
                planned.distro, planned.release, TRUE, planned.ord
            FROM derived_vulnerability_planned_releases AS planned
        ) AS candidate
    ) AS ranked
    WHERE NOT ranked.legacy OR NOT ranked.distro_has_release_state
    ORDER BY ranked.collector_instance_id COLLATE "C", ranked.ecosystem COLLATE "C", ranked.package_id COLLATE "C",
        ranked.source COLLATE "C", CASE WHEN ranked.release IS NULL THEN NULL ELSE ranked.distro END COLLATE "C", ranked.release COLLATE "C",
        ranked.updated_at DESC NULLS LAST, ranked.scope_id
),
derived_vulnerability_source_matched AS (
    SELECT matched.ecosystem, matched.package_id, matched.collector_instance_id,
        COALESCE(matched.scope_id, scope.scope_id) AS scope_id, matched.source,
        matched.updated_at, matched.freshness_state, matched.terminal_status, matched.last_error_class,
        matched.last_success_at, matched.last_attempt_at, matched.next_retry_at,
        matched.result_count, matched.warning_count, matched.distro, matched.release
    FROM derived_vulnerability_source_chosen AS matched
    LEFT JOIN derived_vulnerability_planned_scopes AS scope
      ON scope.collector_instance_id = matched.collector_instance_id AND scope.ord = matched.planned_ord
),
`

// readinessDerivedReleaseFleetProbeCTE batches the fleet check candidate (a)
// proved in #10135: one EXISTS per distinct release triple a request resolved,
// over the active os_package facts both the planner and the probe read. The
// probe input is the smallest set whose answer can change a verdict: real
// (not planned-only) Alpine release states that are not fresh, or fresh but
// already past the freshness cutoff so the verdict would age them. Fresh rows
// inside the cutoff never consult the fleet, and neither do pending rows, so
// a release the fleet still carries keeps counting in every plan window.
//
// The predicate inverts the coordinator's release minting exactly: the reader
// ecosystem the planner filters on, the installed name or source name it
// reads, and the Alpine major.minor line it derives from distro_version. Both
// name arms must miss for absence to hold: a row built from another package's
// source still means the release is installed. Migrations 281 and 282 serve
// the two arms; the probe binds no new placeholder.
func readinessDerivedReleaseFleetProbeCTE() string {
	return `derived_vulnerability_release_fleet_probe AS MATERIALIZED (
    SELECT DISTINCT need.package_id, need.distro, need.release, request.package_names
    FROM (
        SELECT DISTINCT chosen.package_id, chosen.distro, chosen.release
        FROM derived_vulnerability_source_chosen AS chosen
        WHERE chosen.release IS NOT NULL
          AND chosen.distro = 'alpine'
          AND chosen.planned_ord IS NULL
          AND (chosen.freshness_state <> 'fresh'
              OR (chosen.last_success_at IS NULL OR chosen.last_success_at <= $22::timestamptz))
    ) AS need
    JOIN derived_request_packages AS request ON request.package_id = need.package_id
),
derived_vulnerability_release_fleet_presence AS MATERIALIZED (
    SELECT probe.package_id, probe.distro, probe.release,
        EXISTS (
            SELECT 1 FROM fact_records AS fact
            JOIN ingestion_scopes AS scope
              ON scope.scope_id = fact.scope_id
             AND scope.active_generation_id = fact.generation_id
            JOIN scope_generations AS generation
              ON generation.scope_id = fact.scope_id
             AND generation.generation_id = fact.generation_id
            WHERE fact.fact_kind = 'vulnerability.os_package'
              AND fact.is_tombstone = FALSE
              AND generation.status = 'active'
              AND LOWER((COALESCE(NULLIF(fact.payload->>'vendor_advisory_source', ''), fact.payload->>'distro')) COLLATE "C") COLLATE "default" = probe.distro
              AND ` + osPackageFleetReleaseLineExpression("fact.payload") + ` = probe.release
              AND (fact.payload->>'name' = ANY(probe.package_names)
               OR fact.payload->>'source_package' = ANY(probe.package_names))
        ) AS fleet_present
    FROM derived_vulnerability_release_fleet_probe AS probe
),
`
}
