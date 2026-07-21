-- 059_relationship_family_candidate_index.sql
--
-- Bounds deferred relationship backfill to facts from extractor families that
-- can produce repository relationships. The predicate is deliberately the
-- exact predicate used by listDeferredScopedRelationshipFactRecordsQuery so
-- PostgreSQL can prove the partial index applies before reading payload bodies.
--
-- Index renamed to the _v2 suffix by issue #5483 C2. The WHERE predicate gained
-- a Flux GitRepository arm (a file fact whose parsed_file_data captured a
-- non-empty flux_git_repositories array). Because this repository RE-EXECUTES
-- every migration on every boot (there is no version ledger) and
-- `CREATE INDEX ... IF NOT EXISTS` matches only the index NAME, a same-name
-- edit would be a silent no-op on any deployment where the original index
-- already exists: it would keep its OLD WHERE forever and never cover the Flux
-- rows. A new name forces the create-with-the-new-predicate exactly once on
-- existing deployments (and once on fresh ones), then IF NOT EXISTS makes it a
-- no-op on subsequent boots. The old-name index is dropped by the sibling
-- migration 068_drop_relationship_family_candidate_index_legacy.sql. Both
-- statements are isolated one-per-file because CREATE/DROP INDEX CONCURRENTLY
-- cannot run inside the implicit transaction a multi-statement string forms.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_relationship_family_scope_generation_idx_v2
    ON fact_records (scope_id, generation_id, observed_at, fact_id)
    WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
      AND (
        fact_kind = 'gcp_cloud_relationship'
        OR lower(COALESCE(payload->>'artifact_type', '')) IN (
          'terraform',
          'terraform_hcl',
          'terraform_template_text',
          'terragrunt',
          'helm',
          'argocd',
          'dockerfile',
          'docker_compose',
          'github_actions_workflow'
        )
        OR lower(COALESCE(payload->>'artifact_type', '')) LIKE 'ansible_%'
        OR lower(COALESCE(
          payload->>'relative_path',
          payload->>'content_path',
          payload->>'file_path',
          payload->>'path',
          ''
        )) ~ '(^|/)(dockerfile(\.[^/]*)?|jenkinsfile(\.[^/]*)?|puppetfile|berksfile)$|(^|/)docker-compose\.ya?ml$|(^|/)compose\.ya?ml$|(^|/)\.github/workflows/[^/]+\.ya?ml$|(^|/)applicationsets?/.*\.ya?ml$|(^|/)argocd/.*\.ya?ml$|(^|/)values([^/]*)\.ya?ml$|(^|/)chart\.ya?ml$|(^|/)kustomization\.ya?ml$|(^|/)(playbooks|roles|group_vars|host_vars|inventories)/|(^|/)inventory($|/)|\.(tf|tf\.json|tfvars|tfvars\.json|hcl|tpl)$'
        OR (CASE
          WHEN lower(COALESCE(payload->>'artifact_type', '')) = 'argocd'
            OR lower(COALESCE(
              payload->>'relative_path',
              payload->>'content_path',
              payload->>'file_path',
              payload->>'path',
              ''
            )) ~ '\.ya?ml$'
          THEN lower(COALESCE(
            payload->>'content',
            payload->>'content_body',
            ''
          )) LIKE '%kind: application%'
            OR lower(COALESCE(
              payload->>'content',
              payload->>'content_body',
              ''
            )) LIKE '%kind: applicationset%'
          ELSE false
        END)
        OR (CASE
          WHEN lower(COALESCE(
            payload->>'relative_path',
            payload->>'content_path',
            payload->>'file_path',
            payload->>'path',
            ''
          )) ~ '\.ya?ml$'
            AND COALESCE(
              payload->>'content',
              payload->>'content_body',
              ''
            ) LIKE '%gitfs_remotes:%'
          THEN COALESCE(
            payload->>'content',
            payload->>'content_body',
            ''
          ) ~ E'(^|\n)gitfs_remotes[[:space:]]*:'
          ELSE false
        END)
        OR (
          jsonb_typeof(payload -> 'parsed_file_data' -> 'flux_git_repositories') = 'array'
          AND jsonb_array_length(payload -> 'parsed_file_data' -> 'flux_git_repositories') > 0
        )
      );
