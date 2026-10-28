-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9379: bring incident_freshness_triggers in the bootstrap migrations up to
-- the incident freshness trigger store's lease/fencing shape (DDL half).
-- The store's EnsureSchema (run only by webhook-listener and workflow-coordinator
-- claimants) added the claim_expires_at lease column, the
-- claim_fencing_token fencing column, and the stale-claim reap index, while
-- databases built by ApplyBootstrap alone (bootstrap-data-plane,
-- bootstrap-index) lack them: claims there fail with "column
-- claim_fencing_token does not exist" and stale-claim recovery is unproven.
--
-- Both statements are no-ops where a listener already ran EnsureSchema. The
-- constant default makes ADD COLUMN a catalog-only change, and the partial
-- index covers only rows in status 'claimed', so it stays small however much
-- handed_off history the table holds.
--
-- Locking (#7961): DDL only. The claimed-row backfill lives in migration
-- 239 so no table-locking DDL shares a file with bulk DML.

ALTER TABLE incident_freshness_triggers
    ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ NULL;

ALTER TABLE incident_freshness_triggers
    ADD COLUMN IF NOT EXISTS claim_fencing_token BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS incident_freshness_triggers_claimed_lease_idx
    ON incident_freshness_triggers (claim_expires_at)
    WHERE status = 'claimed';
