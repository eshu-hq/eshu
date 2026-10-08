-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7777: bring webhook_refresh_triggers in the bootstrap migrations up to the
-- webhook trigger store's own schema. Issue #7661 (PR #7719) added the claim
-- fencing token and the stale-claim reap index only to the store's EnsureSchema, which
-- only webhook-listener runs. The ingester and collector-git claim, hand off,
-- and reap triggers without it, so a database built by ApplyBootstrap alone
-- (bootstrap-data-plane, bootstrap-index) failed every trigger write with
-- "column claim_fencing_token does not exist" until a listener started.
--
-- Both statements are no-ops where a listener already ran EnsureSchema. The
-- constant default makes ADD COLUMN a catalog-only change, and the partial
-- index covers only rows in status 'claimed', so it stays small however much
-- handed_off history the table holds. Building it still scans the whole
-- table while the ADD COLUMN's exclusive lock is held: a table populated by a
-- listener from before #7661 gets the build here instead of at listener
-- start, where EnsureSchema runs the same plain CREATE INDEX. Measured on
-- PostgreSQL 18 with 1,000,000 rows (258 MB heap, 299 claimed): 66 ms plain,
-- 132 ms CONCURRENTLY, 16 kB index, so a separate CONCURRENTLY migration is
-- not worth its extra file.

ALTER TABLE webhook_refresh_triggers
    ADD COLUMN IF NOT EXISTS claim_fencing_token BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS webhook_refresh_triggers_claimed_at_idx
    ON webhook_refresh_triggers (claimed_at ASC)
    WHERE status = 'claimed';
