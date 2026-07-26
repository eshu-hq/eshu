-- #5747 resolves workload filters from current workload-identity facts at
-- query time. Build the scalar selector index concurrently so an upgrade does
-- not block fact ingestion while the existing table is indexed.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_workload_identity_workload_idx
    ON fact_records ((payload->>'workload_id'), fact_id ASC, generation_id)
    WHERE fact_kind = 'reducer_workload_identity'
      AND is_tombstone = FALSE;
