// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// listSupplyChainImpactReadinessQueryScanTier is the OS-package scan tier
// of the readiness query (issues #5467, #8878, #9685): the os_package and
// scanner_worker.analysis CTEs. It lives in its own file only to keep
// readiness_postgres_query_source.go under the 500-line cap; it is
// spliced verbatim into ListReadinessQuery by readiness_postgres_query.go.
const listSupplyChainImpactReadinessQueryScanTier = `
os_package_active AS (
    -- vulnerability.os_package facts carry no image identity of their own
    -- (distro/package identity only); the scanned image is resolved through
    -- the sibling scanner_worker.analysis fact in the SAME scan scope below,
    -- scope-level across committed generations (issue #8878), unlike the
    -- reducer's pair-keyed SubjectDigest stamp (issue #5463).
    SELECT fact.payload, fact.observed_at, fact.scope_id, fact.generation_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($15::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
scanner_worker_analysis_in_scope AS (
    -- Same-scope image-identity evidence across committed generations, NOT
    -- the active generation alone (issue #8878): sibling scanner tiers
    -- write generations ~0.5ms apart under one image scope, superseding
    -- the generation carrying the only analysis fact. The generation must
    -- still be committed (active or superseded) and the fact not
    -- tombstoned; the digest/image_ref anchor below bounds the read.
    SELECT fact.payload, fact.observed_at, fact.scope_id, fact.generation_id
    FROM fact_records AS fact
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($16::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status IN ('active', 'superseded')
),
scanner_worker_analysis AS (
    -- Present whenever a scan completed for the requested image, whether or
    -- not it found any installed OS packages. This is the "was this image
    -- ever scanned" signal (issue #5467); the scope-wide CTE above lets a
    -- superseded generation's completed scan still prove it (#8878).
    SELECT
        'scanner_worker.analysis' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM scanner_worker_analysis_in_scope
    WHERE payload->>'image_digest' IN (SELECT digest FROM target_image_digests)
       OR ($14 <> '' AND payload->>'image_reference' = $14)
),
scope_with_multiple_digests AS (
    -- Scopes whose committed analyses name more than one distinct image
    -- digest (issue #9685): scope IDs come from operator target
    -- configuration, so a rescan for a new digest lands a new generation
    -- under the SAME scope, and os_package rows carry no digest of their
    -- own. Scope-level sibling attribution is ambiguous there, so this CTE
    -- gates the scope-level fallback below to provably single-digest
    -- scopes. Analyses with no digest neither mix a scope nor resolve it.
    SELECT scope_id
    FROM scanner_worker_analysis_in_scope
    WHERE COALESCE(payload->>'image_digest', '') <> ''
    GROUP BY scope_id
    HAVING COUNT(DISTINCT payload->>'image_digest') > 1
),
vulnerability_os_package AS (
    -- Counted only for ACTIVE-generation os_package facts attributed to the
    -- requested image through a digest-matching scanner_worker.analysis, so
    -- an unrelated scope's rows never count. Two keys, in order:
    --
    -- Pair-keyed primary (issue #9685): the row's own generation carries a
    -- matching analysis, so the row is attributed through the scan that
    -- produced it — the same ScopeID+GenerationID key the reducer stamps
    -- as SubjectDigest (#5463). This is the only key used on multi-digest
    -- scopes, where scope-level attribution would count another image's
    -- packages for an old-digest request.
    --
    -- Both-anchor rule (issue #9691): target_image_digests unions the
    -- subject digest with every identity digest matching the image
    -- reference, so on a rescanned scope a both-anchor request would pull
    -- the other generation's digest into the set and the digest arm would
    -- match it. When the request carries a subject digest ($12), the
    -- sibling analysis must carry that exact digest; the reference only
    -- resolves unanchored (ref-only) requests. This matches
    -- resolveReadinessImageDigests, which intersects digest+ref.
    --
    -- Scope-level fallback (issue #8878) for provably single-digest scopes
    -- only: sibling tiers write generations ~0.5ms apart, so the surviving
    -- active generation's rows carry no image identity of their own while
    -- the digest-matching analysis sits superseded in the same scope.
    -- Counted rows stay active-only (the lifecycle the reducer's advisory
    -- reader enforces); superseded generations contribute identity, never
    -- current package evidence.
    -- Deliberately EXISTS, not JOIN: a JOIN fans out to
    -- COUNT(os_package) * COUNT(matching analyses) across analyzers and
    -- sibling generations; EXISTS counts each os_package fact at most once.
    SELECT
        'vulnerability.os_package' AS family,
        COUNT(*)::int AS fact_count,
        MAX(os_package.observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM os_package_active AS os_package
    WHERE EXISTS (
        SELECT 1
        FROM scanner_worker_analysis_in_scope AS analysis
        WHERE analysis.scope_id = os_package.scope_id
          AND analysis.generation_id = os_package.generation_id
          -- Both-anchor rule: a digest-anchored request only resolves
          -- through the analysis carrying that digest; the reference arm
          -- below serves ref-only requests.
          AND ($12 = '' OR analysis.payload->>'image_digest' = $12)
          AND (
              analysis.payload->>'image_digest' IN (SELECT digest FROM target_image_digests)
              -- The image_reference arm is withheld on multi-digest scopes
              -- (issue #9691 P1): both generations of a rescan share one
              -- reference, so a ref match alone would attribute another
              -- image's packages to an old-digest request.
              OR ($14 <> '' AND analysis.payload->>'image_reference' = $14
                  AND NOT EXISTS (
                      SELECT 1
                      FROM scope_with_multiple_digests AS guarded
                      WHERE guarded.scope_id = os_package.scope_id
                  ))
          )
      )
       OR (
        NOT EXISTS (
            SELECT 1
            FROM scope_with_multiple_digests AS mixed
            WHERE mixed.scope_id = os_package.scope_id
        )
        AND EXISTS (
            SELECT 1
            FROM scanner_worker_analysis_in_scope AS analysis
            WHERE analysis.scope_id = os_package.scope_id
              -- Both-anchor rule, as in the pair-keyed branch above.
              AND ($12 = '' OR analysis.payload->>'image_digest' = $12)
              AND (
                  analysis.payload->>'image_digest' IN (SELECT digest FROM target_image_digests)
                  OR ($14 <> '' AND analysis.payload->>'image_reference' = $14)
              )
        )
      )
)
`
