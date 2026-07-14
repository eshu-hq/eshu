-- 059_relationship_family_candidate_index.sql
--
-- Bounds deferred relationship backfill to facts from extractor families that
-- can produce repository relationships. The predicate is deliberately the
-- exact predicate used by listDeferredScopedRelationshipFactRecordsQuery so
-- PostgreSQL can prove the partial index applies before reading payload bodies.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_relationship_family_scope_generation_idx
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
      );
