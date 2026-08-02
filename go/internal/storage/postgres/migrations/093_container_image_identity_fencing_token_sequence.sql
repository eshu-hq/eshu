-- #5874: container_image_identity's fencing token was the REDUCER HOST'S wall
-- clock (containerImageIdentityFencingToken, write.EvidenceAsOf.UTC().UnixMicro()).
-- With modest clock skew between reducer replicas, a worker that started
-- LATER on a fast-clock host can carry a SMALLER token than a worker that
-- started EARLIER on a correct clock, so the write conflict guard
-- (reducerFactBatchInsertQuery, fact_records.fencing_token <=
-- EXCLUDED.fencing_token) can admit a genuinely staler pass over a fresher
-- one -- the exact inversion #5848/#5875 already fixed for the sibling
-- aws_cloud_runtime_drift domain. See go/internal/reducer/
-- container_image_identity_admission.go's containerImageIdentityFencingToken
-- doc comment.
--
-- This sequence replaces the wall-clock token, mirroring migration
-- 089_aws_cloud_runtime_drift_fencing_token_sequence.sql exactly. Every
-- reducer replica issues nextval() against the SAME shared Postgres instance,
-- so the value reflects real invocation order (which pass called nextval()
-- more recently), never any individual host's clock. The token is still
-- issued at EVIDENCE-READ time (in ContainerImageIdentityHandler.Handle,
-- before the first fact load), not at write-commit time: a write-time token
-- would order admission by COMMIT order instead of evidence recency, which
-- would rank a stalled worker highest -- the same inversion this migration
-- exists to close, relocated rather than fixed.
CREATE SEQUENCE IF NOT EXISTS container_image_identity_fencing_token_seq;

-- The seed query below (`MAX(fencing_token) FROM fact_records WHERE
-- fact_kind = 'reducer_container_image_identity'`) is indexed by
-- fact_records_container_image_identity_fencing_token_idx, a SEPARATE
-- migration (094_fact_records_container_image_identity_fencing_token_idx.sql):
-- that index needs CREATE INDEX CONCURRENTLY, which cannot run inside a
-- transaction block, and ApplyBootstrap sends each migration FILE's full SQL
-- (every statement here: this CREATE SEQUENCE and the DO block below) as one
-- multi-statement string to a single ExecContext call -- Postgres's simple
-- query protocol implicitly wraps a multi-statement string in a transaction,
-- so CONCURRENTLY cannot share a file with any other statement. See that
-- migration's comment for the measured impact (mirrors migration 090's
-- methodology and result for the aws_cloud_runtime_drift domain, re-run here
-- for reducer_container_image_identity: EXPLAIN (ANALYZE, BUFFERS) on the
-- seed query dropped from a Bitmap Heap Scan over
-- fact_records_scope_generation_idx (cost ~6079, ~1.5ms on a 200,000-row /
-- 1,000-matching synthetic corpus) to an Index Only Scan Backward + LIMIT 1
-- (cost ~0.42, ~0.03ms) after the partial index, roughly the same three- to
-- four-order-of-magnitude drop migration 090 measured).
--
-- Because migration 093 (this file) applies before 094 by filename order, the
-- FIRST bootstrap apply's seed query below still runs unindexed once; every
-- later bootstrap-SQL re-apply (every subsequent reducer process start, not a
-- one-shot ledger) runs it against the now-existing index from 094.
--
-- Seed the sequence above the highest wall-clock-derived token already stored
-- for this domain (both the admission watermark table and any already-written
-- fact_records rows), so on a live, previously-deployed database the first
-- sequence-issued token is guaranteed fresher than every pre-existing
-- watermark -- container_image_identity's existing tokens are UnixMicro
-- values (~1.7-1.8e15 on any database ingested in the last several years),
-- vastly larger than a bare sequence's MINVALUE-1 default, so a naive
-- unseeded sequence would have every post-migration write rejected by the
-- `stored <= new` admission check against the much larger pre-existing
-- wall-clock watermark. Guarded against the sequence's OWN current position
-- (not just run unconditionally) so re-applying this bootstrap SQL on every
-- process start can only ever advance the sequence forward, never regress it
-- back below values already issued to in-flight or committed writers.
DO $$
DECLARE
    watermark_floor BIGINT;
    current_last_value BIGINT;
BEGIN
    SELECT GREATEST(
        COALESCE((SELECT MAX(fencing_token) FROM container_image_identity_write_admission), 0),
        COALESCE((SELECT MAX(fencing_token) FROM fact_records WHERE fact_kind = 'reducer_container_image_identity'), 0)
    ) INTO watermark_floor;
    SELECT last_value INTO current_last_value FROM container_image_identity_fencing_token_seq;
    IF watermark_floor > current_last_value THEN
        PERFORM setval('container_image_identity_fencing_token_seq', watermark_floor + 1, false);
    END IF;
END $$;
