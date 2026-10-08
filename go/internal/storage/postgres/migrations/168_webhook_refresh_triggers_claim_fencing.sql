-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7777: bring webhook_refresh_triggers in the bootstrap migrations up to the
-- webhook trigger store's own schema. #7661/#7719 added the claim fencing
-- token and the stale-claim reap index only to the store's EnsureSchema, which
-- only webhook-listener runs. The ingester and collector-git claim, hand off,
-- and reap triggers without it, so a database built by ApplyBootstrap alone
-- (bootstrap-data-plane, bootstrap-index) failed every trigger write with
-- "column claim_fencing_token does not exist" until a listener started.
--
-- Both statements are no-ops where a listener already ran EnsureSchema. The
-- constant default makes ADD COLUMN a catalog-only change, and the partial
-- index covers only rows in status 'claimed', so it stays small however much
-- handed_off history the table holds.

ALTER TABLE webhook_refresh_triggers
    ADD COLUMN IF NOT EXISTS claim_fencing_token BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS webhook_refresh_triggers_claimed_at_idx
    ON webhook_refresh_triggers (claimed_at ASC)
    WHERE status = 'claimed';
