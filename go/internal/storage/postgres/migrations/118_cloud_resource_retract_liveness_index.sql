-- #6887: serve the generation-diff retract's global live-check as a point
-- lookup. LiveAdmissionCloudUIDs probes fact_records by
-- payload->>'cloud_resource_uid' within the reducer_cloud_resource_identity /
-- non-tombstone slice; without this partial expression index the planner
-- filters the whole fact-kind slice per candidate (plan cost 617 vs 12 with
-- the index at 2k kind rows, EXPLAIN (ANALYZE, BUFFERS) on the parity
-- battery — see docs/internal/evidence/6887-cloud-retract-liveness.md). At
-- 150k rows per kind the filter cost scales with kind cardinality while the
-- index probe stays constant, which is what keeps the per-candidate check
-- bounded on the materialization write path.
-- Boundary (see #6946): the planner serves this index for small candidate
-- arrays; at lock-chunk size (500 candidates) it estimates ~1,000 rows per
-- element and walks ingestion_scopes instead, ~204k buffers per chunk at
-- 2,000 scopes x 100 admitted facts. The constant-probe argument holds on
-- the index path only.
--
-- Partial (not full-expression): the predicate matches the probe exactly, so
-- unrelated kinds pay no maintenance. CONCURRENTLY so bootstrap never blocks
-- writers; IF NOT EXISTS for rerun safety, matching 073/086 precedent.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_cloud_retract_admission_uid_idx
    ON fact_records ((payload->>'cloud_resource_uid'))
    WHERE fact_kind = 'reducer_cloud_resource_identity'
      AND is_tombstone = FALSE;
