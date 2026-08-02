-- The completion fanout updates current-generation consumer intents in place.
-- fact_work_items is populated and write-hot, so this index must remain the
-- only statement in this file: CONCURRENTLY cannot run in a transaction block.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_work_items_cross_scope_source_idx
    ON fact_work_items (
        domain, scope_id, generation_id, status, work_item_id
    )
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running', 'succeeded')
      AND domain IN ('ci_cd_run_correlation', 'supply_chain_impact');
