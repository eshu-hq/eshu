-- #5875 P1 (round-5 hostile review, codex): aws_cloud_runtime_drift's
-- cross-worker admission fence used evidenceAsOf.UnixMicro() -- the REDUCER
-- HOST'S wall clock -- as the ordering token. With modest clock skew between
-- reducer replicas, an older worker on a fast-clock host can carry a LARGER
-- token than a later worker on a correct clock, so if the fresher worker
-- commits first, the stale older worker is admitted afterward and its retire
-- replaces correct truth with stale truth -- defeating the whole point of the
-- admission CAS (#5848).
--
-- This sequence replaces the wall-clock token. Every reducer replica issues
-- nextval() against the SAME shared Postgres instance, so the value reflects
-- real invocation order (which pass called nextval() more recently), never
-- any individual host's clock. See
-- go/internal/reducer/aws_cloud_runtime_drift_admission.go's
-- awsCloudRuntimeDriftFencingToken doc comment for why the token is still
-- issued at EVIDENCE-READ time (in AWSCloudRuntimeDriftHandler.Handle, before
-- the evidence load), not at write-commit time: a write-time token would
-- restore ordering by commit order instead of evidence recency, silently
-- reintroducing the original #5848 bug (a stalled worker's stale evidence
-- landing after a fresher worker's committed write) while fixing this one.
CREATE SEQUENCE IF NOT EXISTS aws_cloud_runtime_drift_fencing_token_seq;

-- Round-6 review (P2): the seed query below (`MAX(fencing_token) FROM
-- fact_records WHERE fact_kind = ...`) has no supporting index without this.
-- `fact_records_scope_generation_idx` carries `fact_kind` as its THIRD
-- column, so the planner resolves the predicate via a full index traversal
-- (every fact_records entry, not a seek) -- the identical anti-pattern
-- `#3389` already paid down elsewhere in this file with per-fact_kind
-- partial indexes. Because this seed query re-runs on every bootstrap-SQL
-- re-apply (every reducer process start, not once), its cost scales with
-- TOTAL fact_records size on every restart, not just at first deploy.
--
-- Proven with EXPLAIN (ANALYZE, BUFFERS) against a synthetic 200,000-row
-- fact_records (40 fact_kinds, 1,000 rows matching
-- reducer_aws_cloud_runtime_drift_finding -- the reviewer's own
-- representative shape) on an isolated scratch Postgres 16 instance:
--
--   OLD (fact_records_scope_generation_idx, fact_kind third column):
--     Aggregate (cost=3788.52..3788.53 rows=1) actual time=0.291ms
--       -> Index Scan using fact_records_scope_generation_idx
--          Index Cond: (fact_kind = 'reducer_aws_cloud_runtime_drift_finding')
--          (scans every fact_records index entry; cost scales with TABLE size)
--
--   NEW (this partial index, leading on fencing_token):
--     Result (cost=0.31..0.32 rows=1) actual time=0.039ms
--       -> Limit
--            -> Index Only Scan Backward using
--               fact_records_aws_cloud_runtime_drift_fencing_token_idx
--               Index Cond: (fencing_token IS NOT NULL)
--               Heap Fetches: 0
--     (Postgres's standard MAX()-via-backward-index-scan-LIMIT-1 rewrite;
--     cost is O(1) in fact_kind's own row count, not the whole table)
--
-- ~12,000x lower planner cost, and -- unlike the #3389 precedent's
-- (scope_id, generation_id, fact_id) enumeration shape, which still scales
-- with the MATCHING fact_kind's row count -- this shape is a leading-column
-- MAX() index, so cost does not grow with fact_records' total size OR with
-- how many reducer_aws_cloud_runtime_drift_finding rows accumulate. Building
-- the index is a plain CREATE INDEX (no CONCURRENTLY, matching every other
-- index in this file) and does not rewrite fact_records itself; re-applying
-- this bootstrap SQL is a verified no-op (CREATE INDEX IF NOT EXISTS).
CREATE INDEX IF NOT EXISTS fact_records_aws_cloud_runtime_drift_fencing_token_idx
    ON fact_records (fencing_token)
    WHERE fact_kind = 'reducer_aws_cloud_runtime_drift_finding';

-- Seed the sequence above the highest wall-clock-derived token already
-- stored for this domain (both the admission watermark table and any
-- already-written fact_records rows), so on a live, previously-deployed
-- database the first sequence-issued token is guaranteed fresher than every
-- pre-existing watermark -- otherwise the first several post-migration
-- writes would find their small new sequence values rejected by the
-- `stored <= new` admission check against a much larger pre-existing
-- wall-clock value. Guarded against the sequence's OWN current position
-- (not just run unconditionally) so re-applying this bootstrap SQL on every
-- process start -- this repo's migrations are idempotent bootstrap SQL, not
-- a one-shot ledger -- can only ever advance the sequence forward, never
-- regress it back below values already issued to in-flight or committed
-- writers.
DO $$
DECLARE
    watermark_floor BIGINT;
    current_last_value BIGINT;
BEGIN
    SELECT GREATEST(
        COALESCE((SELECT MAX(fencing_token) FROM aws_cloud_runtime_drift_write_admission), 0),
        COALESCE((SELECT MAX(fencing_token) FROM fact_records WHERE fact_kind = 'reducer_aws_cloud_runtime_drift_finding'), 0)
    ) INTO watermark_floor;
    SELECT last_value INTO current_last_value FROM aws_cloud_runtime_drift_fencing_token_seq;
    IF watermark_floor > current_last_value THEN
        PERFORM setval('aws_cloud_runtime_drift_fencing_token_seq', watermark_floor + 1, false);
    END IF;
END $$;
