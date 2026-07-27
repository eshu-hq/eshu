-- #5747 workload-identity facts can carry workload anchors in entity_keys
-- instead of workload_id. Build the JSONB membership index concurrently so
-- current-runtime filters stay dimension-first without blocking ingestion.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_workload_identity_entity_keys_idx
    ON fact_records USING GIN ((payload->'entity_keys'))
    WHERE fact_kind = 'reducer_workload_identity'
      AND is_tombstone = FALSE;
