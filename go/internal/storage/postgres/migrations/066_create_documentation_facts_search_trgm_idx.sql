-- Accelerate the exact five-field substring expression used by
-- list_documentation_facts without rewriting existing fact rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_facts_search_trgm_idx
    ON fact_records USING GIN ((
        LOWER(
            COALESCE(payload->>'display_name', '') || ' ' ||
            COALESCE(payload->>'title', '') || ' ' ||
            COALESCE(payload->>'heading_text', '') || ' ' ||
            COALESCE(payload->>'content', '') || ' ' ||
            COALESCE(payload->>'target_uri', '')
        )
    ) gin_trgm_ops)
    WHERE fact_kind IN (
        'documentation_source',
        'documentation_document',
        'documentation_section',
        'documentation_link',
        'documentation_entity_mention',
        'documentation_claim_candidate',
        'semantic.documentation_observation'
    )
      AND is_tombstone = FALSE;
