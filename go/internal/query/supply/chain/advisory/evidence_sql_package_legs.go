// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

// This file holds the five-kind slices of the four advisory-evidence
// package legs. Each package OR leg of ListEvidenceQuery (seed package_id,
// seed purl, matched package_id, matched purl) is split in two: the
// affected_package slice stays in evidence_sql.go pinned to
// fact_kind = 'vulnerability.affected_package', and the slice below covers
// the remaining five advisory kinds. GitLab (GLAD) CVE envelopes carry
// package_id (see newGitLabCVEEnvelope), so pinning the legs to
// affected_package alone silently dropped package-matching CVE rows
// (eshu-hq/eshu#9474 P1).
//
// Both IN predicates are retained on every slice: the six-kind IN keeps the
// shared kind-envelope shape the root SQL tests pin, and the five-kind IN
// lets the planner prove the
// fact_records_vulnerability_active_package_lookup_v2_idx /
// fact_records_vulnerability_active_purl_lookup_v2_idx partial predicates.
// Each active-generation EXISTS carries the same anti-flattening guard as
// the legs in evidence_sql.go, keeping it a correlated subplan.
const advisoryEvidenceSeedPackageFiveKindLegs = `
        UNION ALL
        SELECT leg.fact_id, leg.scope_id, leg.generation_id, leg.fact_kind, leg.source_confidence, leg.observed_at, leg.payload, leg.schema_version
        FROM package_ids AS pkg
        JOIN LATERAL (
            SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload, fact.schema_version
            FROM fact_records AS fact
            WHERE fact.payload->>'package_id' = pkg.value
              AND EXISTS (
                  SELECT 1
                  FROM scope_active_generations AS scope
                  WHERE scope.scope_id = fact.scope_id
                    AND scope.active_generation_id = fact.generation_id
                    OFFSET 0
              )
              AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )
              AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_package',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )
              AND fact.fact_kind = ANY($1::text[])
              AND fact.is_tombstone = FALSE
        ) AS leg ON TRUE
        UNION ALL
        SELECT leg.fact_id, leg.scope_id, leg.generation_id, leg.fact_kind, leg.source_confidence, leg.observed_at, leg.payload, leg.schema_version
        FROM package_ids AS pkg
        JOIN LATERAL (
            SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload, fact.schema_version
            FROM fact_records AS fact
            WHERE fact.payload->>'purl' = pkg.value
              AND EXISTS (
                  SELECT 1
                  FROM scope_active_generations AS scope
                  WHERE scope.scope_id = fact.scope_id
                    AND scope.active_generation_id = fact.generation_id
                    OFFSET 0
              )
              AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )
              AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_package',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )
              AND fact.fact_kind = ANY($1::text[])
              AND fact.is_tombstone = FALSE
        ) AS leg ON TRUE
`

const advisoryEvidenceMatchedPackageFiveKindLegs = `
        UNION ALL
        SELECT leg.fact_id, leg.scope_id, leg.generation_id, leg.fact_kind, leg.source_confidence, leg.observed_at, leg.payload, leg.schema_version
        FROM package_ids AS pkg
        JOIN LATERAL (
            SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload, fact.schema_version
            FROM fact_records AS fact
            WHERE fact.payload->>'package_id' = pkg.value
              AND EXISTS (
                  SELECT 1
                  FROM scope_active_generations AS scope
                  WHERE scope.scope_id = fact.scope_id
                    AND scope.active_generation_id = fact.generation_id
                    OFFSET 0
              )
              AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )
              AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_package',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )
              AND fact.fact_kind = ANY($1::text[])
              AND fact.is_tombstone = FALSE
              AND ($4 = '' OR LOWER(fact.payload->>'source') = $4)
        ) AS leg ON TRUE
        UNION ALL
        SELECT leg.fact_id, leg.scope_id, leg.generation_id, leg.fact_kind, leg.source_confidence, leg.observed_at, leg.payload, leg.schema_version
        FROM package_ids AS pkg
        JOIN LATERAL (
            SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload, fact.schema_version
            FROM fact_records AS fact
            WHERE fact.payload->>'purl' = pkg.value
              AND EXISTS (
                  SELECT 1
                  FROM scope_active_generations AS scope
                  WHERE scope.scope_id = fact.scope_id
                    AND scope.active_generation_id = fact.generation_id
                    OFFSET 0
              )
              AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )
              AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_package',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )
              AND fact.fact_kind = ANY($1::text[])
              AND fact.is_tombstone = FALSE
              AND ($4 = '' OR LOWER(fact.payload->>'source') = $4)
        ) AS leg ON TRUE
`
