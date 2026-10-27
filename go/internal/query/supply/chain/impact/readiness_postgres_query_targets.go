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
          )
    ) AS invalid
),
configured_vulnerability_source_targets AS (
    SELECT instance.instance_id AS collector_instance_id,
        NULLIF(TRIM(target->>'scope_id'), '') AS scope_id,
        selector.source,
        NULLIF(LOWER(TRIM(target->>'ecosystem')), '') AS ecosystem,
        selector.source = 'nvd' AND selector.cve_selected AS coverage_eligible
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
        FALSE AS coverage_eligible
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
        BOOL_OR(coverage_eligible) AS coverage_eligible
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
