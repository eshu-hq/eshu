WITH terraform_source AS (
    SELECT
        evidence_id,
        generation_id,
        relationship_type,
        COALESCE(source_entity_id, source_repo_id, '') AS source_key,
        COALESCE(target_entity_id, target_repo_id, '') AS target_key
    FROM relationship_evidence_facts
    WHERE evidence_kind LIKE 'TERRAFORM_%'
       OR evidence_kind LIKE 'TERRAGRUNT_%'
)
SELECT
    'terraform_iac_relationships',
    (SELECT COUNT(*) FROM terraform_source),
    (
        SELECT COUNT(DISTINCT resolved.resolved_id)
        FROM resolved_relationships AS resolved
        JOIN terraform_source AS source
          ON source.generation_id = resolved.generation_id
         AND source.relationship_type = resolved.relationship_type
         AND source.source_key = COALESCE(resolved.source_entity_id, resolved.source_repo_id, '')
         AND source.target_key = COALESCE(resolved.target_entity_id, resolved.target_repo_id, '')
    );
