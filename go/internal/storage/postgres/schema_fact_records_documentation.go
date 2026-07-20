// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const documentationFactRecordReadIndexesSQL = `
CREATE INDEX IF NOT EXISTS fact_records_documentation_sources_observed_idx ON fact_records (observed_at DESC, fact_id DESC) WHERE fact_kind = 'documentation_source' AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_documentation_findings_visible_idx ON fact_records ((payload->>'finding_type'), (payload->>'source_id'), (payload->>'document_id'), (payload->>'status'), (payload->>'truth_level'), (payload->>'freshness_state'), observed_at DESC, fact_id DESC) WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_documentation_facts_search_trgm_idx
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

CREATE INDEX IF NOT EXISTS fact_records_documentation_packets_finding_idx ON fact_records (COALESCE(payload->>'finding_id', payload->'finding'->>'finding_id'), observed_at DESC, fact_id DESC) WHERE fact_kind = 'documentation_evidence_packet' AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_documentation_packets_packet_idx ON fact_records ((payload->>'packet_id'), observed_at DESC, fact_id DESC) WHERE fact_kind = 'documentation_evidence_packet' AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_documentation_target_refs_idx ON fact_records USING GIN (payload jsonb_path_ops) WHERE fact_kind IN ('documentation_entity_mention', 'documentation_claim_candidate', 'documentation_finding') AND is_tombstone = FALSE;
`
