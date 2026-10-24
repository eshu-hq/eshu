
WITH legacy_facts AS MATERIALIZED (
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, '') AS source_uri,
    COALESCE(fact.source_record_id, '') AS source_record_id,
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind IN (
    'vulnerability.cve',
    'vulnerability.affected_package',
    'vulnerability.affected_product',
    'vulnerability.suppression',
    'security_alert.repository_alert',
    'package_registry.package_version',
    'package_registry.vulnerability_hint',
    'reducer_package_consumption_correlation',
    'sbom.component',
    'reducer_sbom_attestation_attachment',
    'reducer_ci_cd_run_correlation',
    'reducer_platform_materialization',
    'reducer_service_catalog_correlation',
    'reducer_workload_identity',
    'oci_registry.image_manifest',
    'oci_registry.image_index',
    'oci_registry.image_tag_observation',
    'oci_registry.image_referrer',
    'file',
    'vulnerability.epss_score',
    'vulnerability.known_exploited'
)
  AND fact.is_tombstone = FALSE
  -- #5237: 'file' is in the scanned kind list ONLY to feed the JS/TS
  -- reachability branch further down, and that branch cannot match unless
  -- $10 (FileRepositoryIDs) is non-empty. $10 is populated only when an
  -- affected package is npm-ecosystem (npmAffectedPackages,
  -- supplychain/core/active_filter.go), so for a
  -- Maven/PyPI/Go/OS-package intent every 'file' row in every active scope
  -- was read and its payload detoasted once per ungated identity predicate
  -- below -- roughly thirteen full detoasts per file fact -- to produce no
  -- match. That is the ~116s active-evidence load the issue reports; a
  -- 448k-file-row proof measured 42.951s before this gate and 0.052s after.
  --
  -- Exactness: with $10 empty a 'file' row cannot satisfy ANY branch of the
  -- disjunction. The suppression-scope branches and the repository branch
  -- are already gated to other fact kinds, and the ungated identity
  -- predicates compare payload keys the file fact contract never carries
  -- (codegraph/v1.File is repo_id/relative_path/parsed_file_data plus
  -- graph_id/graph_kind/is_dependency/language). That premise is pinned by
  -- TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey, which fails
  -- loudly if a contract change ever adds one of those keys to a file fact.
  -- TestSupplyChainImpactFileKindGateLive then runs the whole page statement
  -- against real Postgres with $10 empty, NULL, and populated, and fails if
  -- the gate changes the rows or stops saving the detoast traffic.
  AND (fact.fact_kind <> 'file' OR COALESCE(cardinality($10::text[]), 0) > 0)
  AND generation.status = 'active'
  AND (
      fact.payload->>'package_id' = ANY($1::text[])
      -- $13-$17 (below and at repository_id) are lower(btrim(...))
      -- normalized REPLACEMENTS for what used to be exact-match
      -- ->'scope'->>'X' = ANY($N) predicates here (there is no exact-match
      -- fallback left for package_id/purl/cve_id/subject_digest/
      -- repository_id under "scope" -- $13-$17 fully supersede them, added
      -- for #5466 round-3 review F-6: scopeAnchorMatches
      -- (supplychain/core/scope.go)
      -- compares every vulnerability.suppression scope anchor with
      -- strings.TrimSpace + strings.EqualFold, so a payload of
      -- {"cve_id":"cve-2026-1234"} (lowercase) or a whitespace-padded purl
      -- decodes and matches in Go but was never SELECTED by the prior
      -- exact-match SQL -- the same defect class P1-1/F-4 fixed for
      -- environment/workload_id/service_id, now closed here too. These use
      -- NEW placeholders rather than reusing $1-$3/$5/$8 because those
      -- placeholders are ALSO bound to the top-level (non-"scope") sibling
      -- predicates immediately below, which serve OTHER fact kinds
      -- (vulnerability.affected_package, sbom.component, ...) whose
      -- existing exact-match behavior must not change -- only the
      -- "scope"-nested comparisons were replaced.
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND cardinality($13::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'package_id', U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')) = ANY($13::text[])
      )
      OR fact.payload->>'purl' = ANY($2::text[])
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND cardinality($14::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'purl', U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')) = ANY($14::text[])
      )
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND cardinality($15::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'cve_id', U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')) = ANY($15::text[])
      )
      OR (
          cardinality($4::text[]) > 0
          AND (
              fact.payload->>'advisory_id' = ANY($4::text[])
          )
      )
      OR fact.payload->>'subject_digest' = ANY($5::text[])
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND cardinality($16::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'subject_digest', U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')) = ANY($16::text[])
      )
      OR fact.payload->>'digest' = ANY($5::text[])
      OR fact.payload->>'artifact_digest' = ANY($5::text[])
      OR fact.payload->>'referrer_digest' = ANY($5::text[])
      OR fact.payload->>'resolved_digest' = ANY($5::text[])
      OR fact.payload->>'cpe' = ANY($6::text[])
      OR fact.payload->>'criteria' = ANY($6::text[])
      OR fact.payload->>'document_id' = ANY($7::text[])
      OR (
          fact.fact_kind IN (
              'vulnerability.suppression',
              'reducer_package_consumption_correlation',
              'reducer_ci_cd_run_correlation',
              'reducer_platform_materialization',
              'reducer_service_catalog_correlation',
              'reducer_workload_identity'
          )
          AND (
              fact.payload->>'repository_id' = ANY($8::text[])
              OR fact.payload->>'repo_id' = ANY($8::text[])
              -- $17 is the lower(btrim(...)) normalized replacement for
              -- what was an exact-match ->'scope'->>'repository_id' =
              -- ANY($8) predicate here. It binds the same repository filter
              -- values after normalization, so it strictly supersedes the
              -- old exact-match comparison (#5466 round-3 review F-6).
              OR (
                  fact.fact_kind = 'vulnerability.suppression'
                  AND cardinality($17::text[]) > 0
                  AND lower(btrim(fact.payload->'scope'->>'repository_id', U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')) = ANY($17::text[])
              )
              OR fact.scope_id = ANY($8::text[])
              OR fact.payload->>'scope_id' = ANY($8::text[])
              OR scope.source_key = ANY($8::text[])
              OR scope.payload->>'repo_id' = ANY($8::text[])
              OR scope.payload->>'id' = ANY($8::text[])
          )
      )
      OR (
          fact.fact_kind = 'file'
          AND (
              fact.payload->>'repository_id' = ANY($10::text[])
              OR fact.payload->>'repo_id' = ANY($10::text[])
              OR fact.payload->'scope'->>'repository_id' = ANY($10::text[])
              OR fact.scope_id = ANY($10::text[])
              OR fact.payload->>'scope_id' = ANY($10::text[])
              OR scope.source_key = ANY($10::text[])
              OR scope.payload->>'repo_id' = ANY($10::text[])
              OR scope.payload->>'id' = ANY($10::text[])
          )
          AND LOWER(COALESCE(
              fact.payload->'parsed_file_data'->>'language',
              fact.payload->>'language',
              ''
          )) IN ('javascript', 'jsx', 'typescript', 'tsx')
      )
      OR fact.payload->>'image_ref' = ANY($9::text[])
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          -- $18 replaces the exact nested advisory_id comparison with the
          -- same case-folding and Unicode TrimSpace semantics the reducer
          -- applies. Deployment context is deliberately absent here:
          -- environment, workload_id, and service_id only narrow
          -- suppressions discovered through an identity anchor.
          AND cardinality($18::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'advisory_id', U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')) = ANY($18::text[])
      )
  )
)
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    fact.source_uri,
    fact.source_record_id,
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM legacy_facts AS fact
WHERE (
    $11 = ''
    OR (
        fact.fact_kind = 'vulnerability.suppression',
        convert_to(fact.fact_id, 'UTF8')
    ) > (
        $19::boolean,
        convert_to($11, 'UTF8')
    )
)
-- #5466 round-8 review F-3: every non-suppression row sorts before every
-- vulnerability.suppression row (the boolean ASC term), so the row cap
-- below (ListActiveSupplyChainImpactFacts) can bound ONLY the suppression
-- tail without ever truncating vulnerability.cve/affected_package/sbom.
-- component/... evidence -- see that function's doc for the full
-- reasoning. The compound keyset cursor ($19 + $11, a Postgres row-value
-- comparison) is required to paginate correctly against this two-part
-- ordering; a locale-sensitive "fact.fact_id > $11" cursor would skip or repeat rows
-- once pagination crosses from the non-suppression group into the
-- suppression group.
ORDER BY
    (fact.fact_kind = 'vulnerability.suppression') ASC,
    convert_to(fact.fact_id, 'UTF8') ASC
LIMIT $12
