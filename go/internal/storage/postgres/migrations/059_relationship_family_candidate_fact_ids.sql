CREATE TABLE IF NOT EXISTS relationship_family_candidate_fact_ids (
    fact_id TEXT NOT NULL REFERENCES fact_records(fact_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    PRIMARY KEY (fact_id)
);

CREATE INDEX IF NOT EXISTS relationship_family_candidate_fact_ids_partition_idx
    ON relationship_family_candidate_fact_ids (scope_id, generation_id, fact_id);

INSERT INTO relationship_family_candidate_fact_ids (
    fact_id,
    scope_id,
    generation_id
)
SELECT
    fact_id,
    scope_id,
    generation_id
FROM fact_records AS fact
WHERE fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND fact.is_tombstone = FALSE
  AND (
    fact.fact_kind = 'gcp_cloud_relationship'
    OR lower(COALESCE(fact.payload->>'artifact_type', '')) IN (
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
    OR lower(COALESCE(fact.payload->>'artifact_type', '')) LIKE 'ansible_%'
    OR lower(COALESCE(
      fact.payload->>'relative_path',
      fact.payload->>'content_path',
      fact.payload->>'file_path',
      fact.payload->>'path',
      ''
    )) ~ '(^|/)(dockerfile|jenkinsfile|puppetfile|berksfile)$|(^|/)docker-compose\.ya?ml$|(^|/)compose\.ya?ml$|(^|/)\.github/workflows/[^/]+\.ya?ml$|(^|/)applicationsets?/.*\.ya?ml$|(^|/)argocd/.*\.ya?ml$|(^|/)values([^/]*)\.ya?ml$|(^|/)chart\.ya?ml$|(^|/)kustomization\.ya?ml$|(^|/)(playbooks|roles|group_vars|host_vars|inventories)/|(^|/)inventory($|/)|\.(tf|tf\.json|tfvars|tfvars\.json|hcl|tpl)$'
    OR CASE
      WHEN lower(COALESCE(fact.payload->>'artifact_type', '')) = 'argocd'
        OR lower(COALESCE(
          fact.payload->>'relative_path',
          fact.payload->>'content_path',
          fact.payload->>'file_path',
          fact.payload->>'path',
          ''
        )) ~ '\.ya?ml$'
      THEN lower(COALESCE(
        fact.payload->>'content',
        fact.payload->>'content_body',
        ''
      )) LIKE '%kind: application%'
        OR lower(COALESCE(
          fact.payload->>'content',
          fact.payload->>'content_body',
          ''
        )) LIKE '%kind: applicationset%'
      ELSE FALSE
    END
  )
ON CONFLICT (fact_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id;
