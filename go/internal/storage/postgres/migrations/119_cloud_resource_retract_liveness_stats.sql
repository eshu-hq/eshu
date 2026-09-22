-- #6946: keep the #6887 live-check on its partial index at lock-chunk
-- candidate counts. LiveAdmissionCloudUIDs probes
-- payload->>'cloud_resource_uid' = ANY($1) over the reducer_cloud_resource_identity
-- slice; migration 118 gives it a partial expression index, but the planner
-- does not use a partial index's expression statistics, so the predicate
-- gets the default 0.5% selectivity per array element. With two candidates
-- that still lands on the index; with a 500-candidate lock chunk the
-- estimate reaches most of the table and the planner walks ingestion_scopes
-- instead: 14,115 shared buffers and ~40 ms per chunk at 2,000 scopes x 200
-- admission facts (two generations), held under the chunk's advisory locks.
--
-- Extended statistics on the same expression cover the whole table and are
-- used: with them the 500-candidate probe estimates 516 rows and plans an
-- Index Scan on fact_records_cloud_retract_admission_uid_idx, 1,346 buffers
-- and 1.7 ms; 50 candidates 253 buffers / 0.23 ms; 2 candidates 13 buffers.
-- The same holds for the generic plan pgx's statement cache settles on
-- after five executions, where the walk is far worse (~14,700 buffers,
-- 580-820 ms) and the index plan costs ~2,500 buffers / 1 ms.
-- Measured in a rolled-back transaction on PostgreSQL 18 (three candidate
-- counts, before/after, same seed), recorded in
-- docs/internal/evidence/6887-cloud-retract-liveness.md under "#6946".
-- Migration 103 records a case where CREATE STATISTICS did not move a
-- multi-column join estimate; this is a single-expression equality whose
-- only missing input is the expression's own ndistinct/MCV, which is exactly
-- what the object supplies.
--
-- ANALYZE populates the new statistics immediately so the first retract
-- after bootstrap plans correctly; autovacuum keeps them current afterwards.
-- IF NOT EXISTS for rerun safety.
CREATE STATISTICS IF NOT EXISTS fact_records_cloud_retract_admission_uid_stats
    ON ((payload->>'cloud_resource_uid'))
    FROM fact_records;
ANALYZE fact_records;
