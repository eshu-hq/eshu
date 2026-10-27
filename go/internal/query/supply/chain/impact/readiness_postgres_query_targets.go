// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// readinessConfiguredSourceTargetsCTE resolves opaque target IDs from active
// collector configuration. The 201-row probe below refuses a truncated read.
// Explicit CVE selectors and modified windows are separate source-health
// routes; only an exact NVD CVE selector may certify empty advisory coverage.
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
                WHERE TRIM(cve.id) = $9
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
            'modified_start_unix_us', CASE WHEN NULLIF(TRIM(target->>'modified_start'), '') IS NULL
                THEN 0 ELSE (EXTRACT(EPOCH FROM (target->>'modified_start')::timestamptz) * 1000000)::bigint END,
            'modified_end_unix_us', CASE WHEN NULLIF(TRIM(target->>'modified_end'), '') IS NULL
                THEN 0 ELSE (EXTRACT(EPOCH FROM (target->>'modified_end')::timestamptz) * 1000000)::bigint END,
            'results_per_page', CASE WHEN NULLIF(TRIM(target->>'modified_start'), '') IS NULL AND
                NULLIF(TRIM(target->>'modified_end'), '') IS NULL THEN 0
                ELSE COALESCE(NULLIF(TRIM(target->>'results_per_page'), '')::int, 0) END,
            'endpoint_candidates', endpoints.ids
        ) AS expected_descriptor
        FROM (
            SELECT COALESCE(JSONB_AGG(id ORDER BY id), '[]'::jsonb) AS ids
            FROM (SELECT DISTINCT TRIM(cve.value) AS id
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
derived_vulnerability_source_targets AS (
    SELECT state.collector_instance_id, state.scope_id, state.source,
        NULLIF(LOWER(TRIM(state.ecosystem)), '') AS ecosystem,
        FALSE AS coverage_eligible,
        NULL::jsonb AS expected_descriptor
    FROM vulnerability_source_states AS state
    JOIN collector_instances AS instance
      ON instance.instance_id = state.collector_instance_id
     AND instance.collector_kind = 'vulnerability_intelligence'
     AND instance.enabled AND instance.deactivated_at IS NULL
    WHERE state.source IN ('osv', 'glad')
      AND NULLIF(LOWER(TRIM(state.ecosystem)), '') IN
          (SELECT ecosystem FROM target_vulnerability_source_ecosystems)
      AND EXISTS (
        SELECT 1 FROM (VALUES
            (instance.configuration->'derive_from_owned_packages'),
            (instance.configuration->'derive_from_installed_evidence')
        ) AS derivation(config)
        WHERE derivation.config->>'enabled' = 'true'
          AND derivation.config->'sources' ? state.source
          AND derivation.config->'ecosystems' ? LOWER(TRIM(state.ecosystem))
      )
),
target_vulnerability_source_scopes AS (
    SELECT collector_instance_id, scope_id, source, ecosystem,
        BOOL_OR(coverage_eligible) AS coverage_eligible,
        MIN(expected_descriptor::text)::jsonb AS expected_descriptor
    FROM (
        SELECT * FROM configured_vulnerability_source_targets
        UNION ALL
        SELECT * FROM derived_vulnerability_source_targets
    ) AS relevant
    GROUP BY collector_instance_id, scope_id, source, ecosystem
),
target_vulnerability_source_overflow AS (
    SELECT EXISTS (
        SELECT 1 FROM target_vulnerability_source_scopes
        ORDER BY collector_instance_id, scope_id OFFSET 200 LIMIT 1
    ) AS overflow
),
`
