CREATE TABLE IF NOT EXISTS relationship_reference_candidate_keys (
    fact_id TEXT NOT NULL REFERENCES fact_records(fact_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    source_repo_id TEXT NOT NULL,
    reference_key TEXT NOT NULL,
    PRIMARY KEY (fact_id)
);

CREATE INDEX IF NOT EXISTS relationship_reference_candidate_keys_partition_idx
    ON relationship_reference_candidate_keys (scope_id, generation_id, fact_id);

INSERT INTO relationship_reference_candidate_keys (
    fact_id,
    scope_id,
    generation_id,
    source_repo_id,
    reference_key
)
SELECT
    fact_id,
    scope_id,
    generation_id,
    COALESCE(
        NULLIF(LOWER(TRIM(payload->>'repo_id')), ''),
        CASE
            WHEN scope_id LIKE 'git-repository-scope:%'
                THEN LOWER(TRIM(SUBSTRING(scope_id FROM LENGTH('git-repository-scope:') + 1)))
            ELSE ''
        END
    ) AS source_repo_id,
    '|' ||
    REGEXP_REPLACE(
        REGEXP_REPLACE(LOWER(payload::text), '\.git', '', 'g'),
        '[^a-z0-9._-]+',
        '|',
        'g'
    ) ||
    '|' AS reference_key
FROM fact_records
WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND is_tombstone = FALSE
ON CONFLICT (fact_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    source_repo_id = EXCLUDED.source_repo_id,
    reference_key = EXCLUDED.reference_key;
