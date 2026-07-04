-- 041_aws_gcp_freshness_claim_lease.sql
--
-- Adds a claim_expires_at lease column plus a claim_fencing_token fencing
-- column to aws_freshness_triggers and gcp_freshness_triggers.
-- ClaimQueuedTriggers only ever selected WHERE status = 'queued'; once a
-- trigger flipped to 'claimed' nothing ever moved it back to 'queued' if the
-- handoff loop aborted mid-batch (one bad trigger's plan/handoff error
-- abandoned every remaining claimed row) or the coordinator crashed after
-- claiming but before marking handed-off/failed. The stuck row was
-- permanently stranded: its resource never got its targeted re-scan, with no
-- operator-visible signal (#4576).
--
-- The lease mirrors the workflow_claims lease_expires_at pattern (#4464): a
-- periodic reap query reclaims 'claimed' rows whose lease has expired back to
-- 'queued' so a later handoff tick can retry them. claim_fencing_token fences
-- MarkTriggersHandedOff/MarkTriggersFailed against exactly that reclaim path:
-- without it, a stale claim-holder whose lease expired and was reaped could
-- still complete a trigger a different owner has since re-claimed (raised in
-- PR #4682 review), because the completion queries otherwise only check
-- trigger_id and status = 'claimed'.

ALTER TABLE aws_freshness_triggers ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ NULL;
ALTER TABLE gcp_freshness_triggers ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ NULL;

ALTER TABLE aws_freshness_triggers ADD COLUMN IF NOT EXISTS claim_fencing_token BIGINT NOT NULL DEFAULT 0;
ALTER TABLE gcp_freshness_triggers ADD COLUMN IF NOT EXISTS claim_fencing_token BIGINT NOT NULL DEFAULT 0;

-- Backfill: rows already sitting at 'claimed' when this migration runs
-- predate the lease entirely, so without a backfill they get
-- claim_expires_at = NULL and ReapExpiredTriggerClaims's
-- "claim_expires_at IS NOT NULL" predicate would skip them forever — leaving
-- exactly the already-stuck triggers #4576 exists to rescue permanently
-- stranded (raised in PR #4682 review). Derive an expiry from claimed_at plus
-- the coordinator's default 5-minute lease so a claim that is genuinely still
-- in flight (claimed moments before this migration ran) is not reclaimed out
-- from under a live holder, while anything claimed longer ago than 5 minutes
-- is immediately eligible for the next reap pass.
UPDATE aws_freshness_triggers
SET claim_expires_at = COALESCE(claimed_at, updated_at) + INTERVAL '5 minutes'
WHERE status = 'claimed' AND claim_expires_at IS NULL;

UPDATE gcp_freshness_triggers
SET claim_expires_at = COALESCE(claimed_at, updated_at) + INTERVAL '5 minutes'
WHERE status = 'claimed' AND claim_expires_at IS NULL;

CREATE INDEX IF NOT EXISTS aws_freshness_triggers_claimed_lease_idx
    ON aws_freshness_triggers (claim_expires_at)
    WHERE status = 'claimed';

CREATE INDEX IF NOT EXISTS gcp_freshness_triggers_claimed_lease_idx
    ON gcp_freshness_triggers (claim_expires_at)
    WHERE status = 'claimed';
