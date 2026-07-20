-- Serve the normal unfiltered findings page in deterministic newest-first order.
-- The distinct name makes replay a stable no-op.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_findings_read_idx
    ON fact_records (observed_at DESC, fact_id DESC)
    WHERE fact_kind = 'documentation_finding'
      AND is_tombstone = FALSE;
