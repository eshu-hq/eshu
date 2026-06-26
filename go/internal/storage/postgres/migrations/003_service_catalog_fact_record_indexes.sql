CREATE INDEX IF NOT EXISTS fact_records_service_catalog_correlations_entity_idx
    ON fact_records (
        scope_id,
        (payload->>'provider'),
        (payload->>'entity_ref'),
        (payload->>'outcome'),
        (payload->>'drift_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_service_catalog_correlation'
      AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_service_catalog_correlations_repository_idx
    ON fact_records (
        (payload->>'repository_id'),
        (payload->>'service_id'),
        (payload->>'workload_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_service_catalog_correlation'
      AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_service_catalog_correlations_service_idx
    ON fact_records (
        (payload->>'service_id'),
        (payload->>'repository_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_service_catalog_correlation'
      AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_service_catalog_correlations_candidate_repository_idx
    ON fact_records USING GIN ((payload->'candidate_repository_ids'))
    WHERE fact_kind = 'reducer_service_catalog_correlation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_service_catalog_correlations_owner_idx
    ON fact_records (
        (payload->>'owner_ref'),
        (payload->>'provider'),
        (payload->>'outcome'),
        (payload->>'drift_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_service_catalog_correlation'
      AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_service_catalog_correlations_workload_idx
    ON fact_records (
        (payload->>'workload_id'),
        (payload->>'service_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_service_catalog_correlation'
      AND is_tombstone = FALSE;
