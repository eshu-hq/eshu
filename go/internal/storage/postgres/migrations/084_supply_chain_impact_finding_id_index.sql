-- #5465: explanation requests normally carry the public finding_id emitted in
-- the reducer payload. Bound that exact lookup before ranking source and
-- operator candidates. The query retains a compatibility fallback for raw
-- fact_id and canonical-key callers.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_supply_chain_impact_finding_id_idx
    ON fact_records ((payload->>'finding_id'))
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;
