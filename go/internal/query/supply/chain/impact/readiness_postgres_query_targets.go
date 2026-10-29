// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// readinessRFC3339Pattern accepts the strict RFC 3339 shape the collector's
// time.Parse(time.RFC3339) accepts: an explicit T, an hour of 00-23, minutes
// and seconds of 00-59, an optional dot fraction, and a Z or +-hh:mm zone. A
// value without a zone would resolve in the session TimeZone, and hour 24 or
// second 60 parse in Postgres but not in Go, so each is malformed here.
// pg_input_is_valid then rejects impossible calendar fields such as month 13.
const readinessRFC3339Pattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]+)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`

// Cast guards for the NVD window descriptor. Each predicate raises no error
// for any input (regex match and pg_input_is_valid report soft failures), so
// the planner may evaluate its parts in any order. Every cast of a configured
// window field sits in the THEN arm of a CASE WHEN on its guard, which
// Postgres evaluates before the arm, and invalid_vulnerability_source_targets
// classifies a present value that fails its guard as source_targets_malformed.
// A guard yields NULL for an absent field, so the descriptor falls to 0.
const (
	readinessTargetModifiedStartValidSQL = `(TRIM(target->>'modified_start') ~ '` + readinessRFC3339Pattern + `'
        AND pg_input_is_valid(TRIM(target->>'modified_start'), 'timestamp with time zone'))`
	readinessTargetModifiedEndValidSQL = `(TRIM(target->>'modified_end') ~ '` + readinessRFC3339Pattern + `'
        AND pg_input_is_valid(TRIM(target->>'modified_end'), 'timestamp with time zone'))`
	// results_per_page decodes into a Go int, so it must be a JSON integer
	// (scale 0, which refuses 100.5 and 100.0); the bound keeps the int4 cast
	// in range (the collector caps it at 2000). The CASE keeps the numeric
	// casts off a non-number, and the ELSE makes a non-number fail the guard.
	readinessTargetResultsPerPageValidSQL = `CASE WHEN JSONB_TYPEOF(target->'results_per_page') = 'number'
        THEN SCALE((target->'results_per_page')::numeric) = 0
            AND ABS((target->'results_per_page')::numeric) <= 999999999
        ELSE FALSE END`
)

// readinessConfiguredSourceTargetsCTE resolves opaque target IDs from active
// collector configuration. The 201-row probe below refuses a truncated read.
// Explicit CVE selectors and modified windows are separate source-health
// routes; only an exact NVD CVE selector may certify empty advisory coverage.
// CVE ids compare case-insensitively (upper case on both sides), matching the
// collector's strings.EqualFold selection and the producer's descriptor.
const readinessConfiguredSourceTargetsCTE = `invalid_vulnerability_source_targets AS (
    SELECT EXISTS (
        SELECT 1 FROM collector_instances AS instance
        WHERE instance.collector_kind = 'vulnerability_intelligence'
          AND instance.enabled AND instance.deactivated_at IS NULL
          AND (
              COALESCE(JSONB_TYPEOF(instance.configuration->'targets'), 'null') NOT IN ('array', 'null')
              OR EXISTS (
                  SELECT 1 FROM JSONB_ARRAY_ELEMENTS(
                      CASE WHEN JSONB_TYPEOF(instance.configuration->'targets') = 'array'
                          THEN instance.configuration->'targets' ELSE '[]'::jsonb END
                  ) AS configured(target)
                  WHERE JSONB_TYPEOF(target) <> 'object'
                     OR JSONB_TYPEOF(target->'scope_id') IS DISTINCT FROM 'string'
                     OR NULLIF(TRIM(target->>'scope_id'), '') IS NULL
                     OR JSONB_TYPEOF(target->'source') IS DISTINCT FROM 'string'
                     OR TRIM(target->>'source') NOT IN ('nvd', 'first_epss', 'cisa_kev', 'osv')
                     OR COALESCE(JSONB_TYPEOF(target->'cve_ids'), 'null') NOT IN ('array', 'null')
                     OR COALESCE(JSONB_TYPEOF(target->'fallback_urls'), 'null') NOT IN ('array', 'null')
                     OR (TRIM(target->>'source') IN ('nvd', 'first_epss')
                         AND JSONB_ARRAY_LENGTH(CASE WHEN JSONB_TYPEOF(target->'cve_ids') = 'array'
                             THEN target->'cve_ids' ELSE '[]'::jsonb END) = 0
                         AND (TRIM(target->>'source') = 'first_epss'
                             OR NULLIF(TRIM(target->>'modified_start'), '') IS NULL
                             OR NULLIF(TRIM(target->>'modified_end'), '') IS NULL))
                     OR (TRIM(target->>'source') = 'osv' AND (
                         NULLIF(TRIM(target->>'ecosystem'), '') IS NULL
                         OR JSONB_ARRAY_LENGTH(CASE WHEN JSONB_TYPEOF(target->'queries') = 'array'
                             THEN target->'queries' ELSE '[]'::jsonb END) = 0))
                     OR (NULLIF(TRIM(target->>'modified_start'), '') IS NOT NULL
                         AND NOT ` + readinessTargetModifiedStartValidSQL + `)
                     OR (NULLIF(TRIM(target->>'modified_end'), '') IS NOT NULL
                         AND NOT ` + readinessTargetModifiedEndValidSQL + `)
                     OR (COALESCE(JSONB_TYPEOF(target->'results_per_page'), 'null') <> 'null'
                         AND NOT (` + readinessTargetResultsPerPageValidSQL + `))
              )
              OR JSONB_PATH_EXISTS(instance.configuration,
                  '$.targets[*].cve_ids[*] ? (@.type() != "string" || @ like_regex "^\\s*$")')
              OR JSONB_PATH_EXISTS(instance.configuration,
                  '$.targets[*].fallback_urls[*] ? (@.type() != "string")')
              OR EXISTS (
                  SELECT 1 FROM JSONB_ARRAY_ELEMENTS(
                      CASE WHEN JSONB_TYPEOF(instance.configuration->'targets') = 'array'
                          THEN instance.configuration->'targets' ELSE '[]'::jsonb END
                  ) AS configured(target)
                  GROUP BY TRIM(target->>'scope_id')
                  HAVING COUNT(*) > 1
              )
          )
    ) AS invalid
),
configured_vulnerability_source_targets AS (
    SELECT instance.instance_id AS collector_instance_id,
        NULLIF(TRIM(target->>'scope_id'), '') AS scope_id,
        selector.source,
        NULLIF(LOWER(TRIM(target->>'ecosystem')), '') AS ecosystem,
        selector.source = 'nvd' AND selector.cve_selected AS coverage_eligible,
        descriptor.expected_descriptor
    FROM collector_instances AS instance
    CROSS JOIN LATERAL JSONB_ARRAY_ELEMENTS(
        CASE WHEN JSONB_TYPEOF(instance.configuration->'targets') = 'array'
            THEN instance.configuration->'targets' ELSE '[]'::jsonb END
    ) AS configured(target)
    CROSS JOIN LATERAL (
        SELECT NULLIF(TRIM(target->>'source'), '') AS source,
            CASE WHEN $9 = '' THEN FALSE ELSE EXISTS (
                SELECT 1 FROM JSONB_ARRAY_ELEMENTS_TEXT(
                    CASE WHEN JSONB_TYPEOF(target->'cve_ids') = 'array'
                        THEN target->'cve_ids' ELSE '[]'::jsonb END
                ) AS cve(id)
                WHERE UPPER(TRIM(cve.id)) = UPPER(TRIM($9))
            ) END AS cve_selected
        OFFSET 0
    ) AS selector
    CROSS JOIN LATERAL (
        SELECT JSONB_BUILD_OBJECT(
            'version', 'nvd.v1', 'source', 'nvd',
            'mode', CASE
                WHEN JSONB_ARRAY_LENGTH(cves.ids) > 0 AND
                    (NULLIF(TRIM(target->>'modified_start'), '') IS NOT NULL OR
                     NULLIF(TRIM(target->>'modified_end'), '') IS NOT NULL) THEN 'cve_window'
                WHEN JSONB_ARRAY_LENGTH(cves.ids) > 0 THEN 'cve'
                WHEN NULLIF(TRIM(target->>'modified_start'), '') IS NOT NULL OR
                     NULLIF(TRIM(target->>'modified_end'), '') IS NOT NULL THEN 'window'
                ELSE 'none' END,
            'cve_ids', cves.ids,
            'modified_start_unix_us', CASE WHEN ` + readinessTargetModifiedStartValidSQL + `
                THEN (EXTRACT(EPOCH FROM TRIM(target->>'modified_start')::timestamptz) * 1000000)::bigint ELSE 0 END,
            'modified_end_unix_us', CASE WHEN ` + readinessTargetModifiedEndValidSQL + `
                THEN (EXTRACT(EPOCH FROM TRIM(target->>'modified_end')::timestamptz) * 1000000)::bigint ELSE 0 END,
            'results_per_page', CASE WHEN NULLIF(TRIM(target->>'modified_start'), '') IS NULL AND
                NULLIF(TRIM(target->>'modified_end'), '') IS NULL THEN 0
                WHEN ` + readinessTargetResultsPerPageValidSQL + ` THEN (target->'results_per_page')::numeric::int
                ELSE 0 END,
            'endpoint_candidates', endpoints.ids
        ) AS expected_descriptor
        FROM (
            SELECT COALESCE(JSONB_AGG(id ORDER BY id), '[]'::jsonb) AS ids
            FROM (SELECT DISTINCT UPPER(TRIM(cve.value)) AS id
                FROM JSONB_ARRAY_ELEMENTS_TEXT(CASE WHEN JSONB_TYPEOF(target->'cve_ids') = 'array'
                    THEN target->'cve_ids' ELSE '[]'::jsonb END) AS cve(value)
                WHERE NULLIF(TRIM(cve.value), '') IS NOT NULL) AS canonical
        ) AS cves
        CROSS JOIN LATERAL (
            SELECT COALESCE(JSONB_AGG(endpoint ORDER BY endpoint), '[]'::jsonb) AS ids
            FROM (SELECT DISTINCT COALESCE(NULLIF(RTRIM(TRIM(value), '/'), ''),
                'https://services.nvd.nist.gov/rest/json/cves/2.0') AS endpoint
                FROM (SELECT COALESCE(NULLIF(TRIM(target->>'feed_url'), ''),
                    NULLIF(TRIM(target->>'base_url'), ''),
                    NULLIF(TRIM(target->>'source_uri'), ''), '') AS value
                    UNION ALL
                    SELECT fallback.value FROM JSONB_ARRAY_ELEMENTS_TEXT(
                        CASE WHEN JSONB_TYPEOF(target->'fallback_urls') = 'array'
                            THEN target->'fallback_urls' ELSE '[]'::jsonb END
                    ) AS fallback(value)
                    WHERE NULLIF(RTRIM(TRIM(fallback.value), '/'), '') IS NOT NULL) AS candidates) AS canonical
        ) AS endpoints
        OFFSET 0
    ) AS descriptor
    WHERE instance.collector_kind = 'vulnerability_intelligence'
      AND instance.enabled AND instance.deactivated_at IS NULL
      AND NULLIF(TRIM(target->>'scope_id'), '') IS NOT NULL
      AND (
        ($9 <> '' AND (
          (selector.source = 'nvd' AND (
            selector.cve_selected OR
            NULLIF(TRIM(target->>'modified_start'), '') IS NOT NULL OR
            NULLIF(TRIM(target->>'modified_end'), '') IS NOT NULL))
          OR (selector.source = 'first_epss' AND selector.cve_selected)
          OR selector.source = 'cisa_kev'))
        OR (selector.source IN ('osv', 'glad')
            AND NULLIF(LOWER(TRIM(target->>'ecosystem')), '') IN
                (SELECT ecosystem FROM target_vulnerability_source_ecosystems))
      )
),
target_vulnerability_source_scopes AS (
    SELECT collector_instance_id, scope_id, source, ecosystem,
        BOOL_OR(coverage_eligible) AS coverage_eligible,
        MIN(expected_descriptor::text)::jsonb AS expected_descriptor
    FROM configured_vulnerability_source_targets
    GROUP BY collector_instance_id, scope_id, source, ecosystem
),
` + readinessDerivedSourceStateCTE + `
target_vulnerability_source_overflow AS (
    SELECT EXISTS (
        SELECT 1 FROM (
            SELECT collector_instance_id FROM target_vulnerability_source_scopes
            UNION ALL
            SELECT collector_instance_id FROM derived_vulnerability_source_aggregates
        ) AS relevant
        OFFSET 200 LIMIT 1
    ) AS overflow
),
`

// readinessDerivedSourceStateCTE builds one aggregate source-state row per
// (collector instance, source, ecosystem) for derived owned-package OSV
// scopes (#9227 F7). It never parses scope ids: a request's package keys join
// vulnerability_source_state_packages by equality on the canonical ecosystem
// and package name. $21 carries the request package id aligned with the
// $17/$18 keys; a repository-only read takes its keys from the active
// consumption facts instead. $22 is the freshness cutoff, so the counts are
// aged here; $23..$25 map ecosystem spellings to canonical names, with $25
// marking the spellings the collector accepts in derivation config.
//
// Per requested package the newest matching state wins (updated_at, then
// scope_id); the kept scopes are the distinct winners. Each lateral carries
// OFFSET 0 so the planner keeps one index-only key seek and one primary-key
// state probe per package instead of flattening into a states scan.
const readinessDerivedSourceStateCTE = `readiness_ecosystem_aliases AS (
    SELECT alias.spelling, alias.canonical, alias.derivation::boolean AS derivation
    FROM UNNEST($23::text[], $24::text[], $25::text[]) AS alias(spelling, canonical, derivation)
),
derived_vulnerability_source_config AS (
    SELECT DISTINCT instance.instance_id AS collector_instance_id, 'osv'::text AS source,
        alias.canonical AS ecosystem, alias.spelling AS derivation_ecosystem
    FROM collector_instances AS instance
    CROSS JOIN LATERAL (VALUES
        (instance.configuration->'derive_from_owned_packages'),
        (instance.configuration->'derive_from_installed_evidence')
    ) AS derivation(config)
    CROSS JOIN LATERAL JSONB_ARRAY_ELEMENTS_TEXT(CASE
        WHEN JSONB_TYPEOF(derivation.config->'ecosystems') = 'array'
         AND JSONB_ARRAY_LENGTH(derivation.config->'ecosystems') > 0
        THEN derivation.config->'ecosystems' ELSE '["npm"]'::jsonb END) AS configured(ecosystem)
    JOIN readiness_ecosystem_aliases AS alias
      ON alias.derivation AND alias.spelling = LOWER(TRIM(configured.ecosystem))
    WHERE instance.collector_kind = 'vulnerability_intelligence'
      AND instance.enabled AND instance.deactivated_at IS NULL
      AND derivation.config->>'enabled' = 'true'
      AND (JSONB_TYPEOF(derivation.config->'sources') IS DISTINCT FROM 'array'
           OR JSONB_ARRAY_LENGTH(derivation.config->'sources') = 0
           OR derivation.config->'sources' ? 'osv')
),
derived_request_keys AS (
    SELECT DISTINCT TRIM(key.package_id) AS package_id, key.ecosystem, key.package_name
    FROM UNNEST($21::text[], $17::text[], $18::text[]) AS key(package_id, ecosystem, package_name)
    WHERE NULLIF(TRIM(key.package_id), '') IS NOT NULL
    UNION
    SELECT TRIM(consumption.payload->>'package_id'), alias.canonical,
        LOWER(TRIM(consumption.payload->>'package_name'))
    FROM package_consumption_correlation_active AS consumption
    JOIN readiness_ecosystem_aliases AS alias
      ON alias.spelling = LOWER(TRIM(consumption.payload->>'ecosystem'))
    WHERE $11 <> '' AND NOT $20
      AND consumption.payload->>'repository_id' = $11
      AND NULLIF(TRIM(consumption.payload->>'package_id'), '') IS NOT NULL
      AND NULLIF(TRIM(consumption.payload->>'package_name'), '') IS NOT NULL
),
derived_request_packages AS (
    SELECT key.ecosystem, key.package_id, ARRAY_AGG(DISTINCT key.package_name) AS package_names
    FROM derived_request_keys AS key
    WHERE key.ecosystem IN (SELECT ecosystem FROM derived_vulnerability_source_config)
    GROUP BY key.ecosystem, key.package_id
),
derived_vulnerability_source_matched AS (
    SELECT DISTINCT ON (hit.collector_instance_id, hit.source, request.ecosystem, request.package_id)
        request.ecosystem, request.package_id, hit.*
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
    ORDER BY hit.collector_instance_id, hit.source, request.ecosystem, request.package_id,
        hit.updated_at DESC, hit.scope_id
),
derived_vulnerability_source_kept AS (
    SELECT kept.*, CASE kept.aged_freshness WHEN 'rate_limited' THEN 0 WHEN 'failed' THEN 1
        WHEN 'partial' THEN 2 WHEN 'pending' THEN 3 WHEN 'stale' THEN 4 WHEN 'fresh' THEN 5 ELSE 3 END AS severity
    FROM (
        SELECT DISTINCT ON (collector_instance_id, source, ecosystem, scope_id) *,
            CASE WHEN freshness_state = 'fresh'
                AND (last_success_at IS NULL OR last_success_at <= $22::timestamptz)
                THEN 'stale' ELSE freshness_state END AS aged_freshness
        FROM derived_vulnerability_source_matched
        ORDER BY collector_instance_id, source, ecosystem, scope_id
    ) AS kept
),
derived_vulnerability_source_totals AS (
    SELECT config.collector_instance_id, config.source, config.ecosystem,
        MIN(config.derivation_ecosystem) AS derivation_ecosystem,
        COUNT(DISTINCT request.package_id) AS requested_packages,
        COUNT(DISTINCT matched.package_id) AS matched_packages
    FROM derived_vulnerability_source_config AS config
    JOIN derived_request_packages AS request ON request.ecosystem = config.ecosystem
    LEFT JOIN derived_vulnerability_source_matched AS matched
      ON matched.collector_instance_id = config.collector_instance_id
     AND matched.source = config.source
     AND matched.ecosystem = config.ecosystem
     AND matched.package_id = request.package_id
    GROUP BY config.collector_instance_id, config.source, config.ecosystem
),
derived_vulnerability_source_aggregates AS (
    SELECT totals.collector_instance_id, totals.source, totals.derivation_ecosystem,
        totals.requested_packages - totals.matched_packages AS unmatched_packages,
        totals.matched_packages,
        COUNT(kept.scope_id) AS scope_count,
        (ARRAY_AGG(kept.aged_freshness ORDER BY kept.severity, kept.last_success_at NULLS FIRST, kept.scope_id))[1] AS worst_freshness,
        (ARRAY_AGG(kept.terminal_status ORDER BY kept.severity, kept.last_success_at NULLS FIRST, kept.scope_id))[1] AS worst_terminal,
        (ARRAY_AGG(kept.last_error_class ORDER BY kept.severity, kept.last_success_at NULLS FIRST, kept.scope_id))[1] AS worst_error,
        MIN(kept.severity) AS worst_severity,
        MIN(kept.last_success_at) AS last_success_at,
        MAX(kept.last_attempt_at) AS last_attempt_at,
        MIN(kept.next_retry_at) AS next_retry_at,
        MAX(kept.updated_at) AS updated_at,
        COALESCE(SUM(kept.result_count), 0)::int AS result_count,
        COALESCE(SUM(kept.warning_count), 0)::int AS warning_count,
        JSONB_BUILD_OBJECT('derived', TRUE,
            'scope_count', COUNT(kept.scope_id),
            'package_count', totals.matched_packages,
            'unmatched_package_count', totals.requested_packages - totals.matched_packages,
            'fresh_count', COUNT(*) FILTER (WHERE kept.aged_freshness = 'fresh'),
            'stale_count', COUNT(*) FILTER (WHERE kept.aged_freshness = 'stale'),
            'failed_count', COUNT(*) FILTER (WHERE kept.aged_freshness = 'failed'),
            'rate_limited_count', COUNT(*) FILTER (WHERE kept.aged_freshness = 'rate_limited'),
            'partial_count', COUNT(*) FILTER (WHERE kept.aged_freshness = 'partial'),
            'pending_count', COUNT(*) FILTER (WHERE kept.aged_freshness = 'pending'),
            'scope_ids', COALESCE(TO_JSONB((ARRAY_AGG(kept.scope_id ORDER BY kept.severity,
                kept.last_success_at NULLS FIRST, kept.scope_id) FILTER (WHERE kept.scope_id IS NOT NULL))[1:10]),
                '[]'::jsonb)) AS derived_detail
    FROM derived_vulnerability_source_totals AS totals
    LEFT JOIN derived_vulnerability_source_kept AS kept
      ON kept.collector_instance_id = totals.collector_instance_id
     AND kept.source = totals.source
     AND kept.ecosystem = totals.ecosystem
    GROUP BY totals.collector_instance_id, totals.source, totals.ecosystem, totals.derivation_ecosystem,
        totals.requested_packages, totals.matched_packages
),
-- A requested package with no derived scope reads as pending: the aggregate
-- is never healthier than its least-covered package.
derived_vulnerability_source_state_candidates AS (
    SELECT collector_instance_id,
        'derived://' || source || '/' || derivation_ecosystem AS scope_id,
        source, derivation_ecosystem AS ecosystem,
        NULL::timestamptz AS window_start, NULL::timestamptz AS window_end,
        last_attempt_at, last_success_at, next_retry_at,
        CASE WHEN unmatched_packages > 0 AND COALESCE(worst_severity, 3) >= 3 THEN NULL ELSE worst_error END AS last_error_class,
        CASE WHEN unmatched_packages > 0 AND COALESCE(worst_severity, 3) >= 3 THEN 'pending' ELSE worst_freshness END AS freshness_state,
        CASE WHEN unmatched_packages > 0 AND COALESCE(worst_severity, 3) >= 3 THEN 'pending' ELSE worst_terminal END AS terminal_status,
        result_count, warning_count, updated_at,
        FALSE AS coverage_eligible, FALSE AS coverage_zero, FALSE AS coverage_missing,
        FALSE AS multi_cve_target,
        derived_detail
    FROM derived_vulnerability_source_aggregates
),
`
