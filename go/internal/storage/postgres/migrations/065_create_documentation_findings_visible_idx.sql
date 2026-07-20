-- Match the documentation findings read after denied evidence became an honest
-- disclosed row instead of a SQL-filtered row.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_findings_visible_idx
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
