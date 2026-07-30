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
