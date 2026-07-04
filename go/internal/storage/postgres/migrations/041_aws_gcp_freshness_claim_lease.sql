-- 041_aws_gcp_freshness_claim_lease.sql
--
-- Adds a claim_expires_at lease column to aws_freshness_triggers and
-- gcp_freshness_triggers. ClaimQueuedTriggers only ever selected
-- WHERE status = 'queued'; once a trigger flipped to 'claimed' nothing ever
-- moved it back to 'queued' if the handoff loop aborted mid-batch (one bad
-- trigger's plan/handoff error abandoned every remaining claimed row) or the
-- coordinator crashed after claiming but before marking handed-off/failed.
-- The stuck row was permanently stranded: its resource never got its
-- targeted re-scan, with no operator-visible signal (#4576).
--
-- The lease mirrors the workflow_claims lease_expires_at pattern (#4464): a
-- periodic reap query reclaims 'claimed' rows whose lease has expired back to
-- 'queued' so a later handoff tick can retry them.

ALTER TABLE aws_freshness_triggers ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ NULL;
ALTER TABLE gcp_freshness_triggers ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS aws_freshness_triggers_claimed_lease_idx
    ON aws_freshness_triggers (claim_expires_at)
    WHERE status = 'claimed';

CREATE INDEX IF NOT EXISTS gcp_freshness_triggers_claimed_lease_idx
    ON gcp_freshness_triggers (claim_expires_at)
    WHERE status = 'claimed';
