-- Serve fully selective six-filter findings pages without sacrificing the unfiltered ordering path.
-- The distinct name makes replay a stable no-op.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_findings_filter_idx
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
