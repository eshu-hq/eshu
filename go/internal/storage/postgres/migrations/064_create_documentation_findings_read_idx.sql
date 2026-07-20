-- Create the corrected read index under a new name before retiring the legacy
-- ACL-filtered index. The distinct name makes bootstrap replay a stable no-op.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_findings_read_idx
    ON fact_records (
        (payload->>'finding_type'),
        (payload->>'source_id'),
        (payload->>'document_id'),
        (payload->>'status'),
        (payload->>'truth_level'),
        (payload->>'freshness_state'),
        observed_at DESC,
        fact_id DESC
    )
    WHERE fact_kind = 'documentation_finding'
      AND is_tombstone = FALSE;
