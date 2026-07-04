// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const gcpFreshnessSchemaSQL = `
CREATE TABLE IF NOT EXISTS gcp_freshness_triggers (
    trigger_id TEXT NOT NULL,
    delivery_key TEXT NOT NULL,
    freshness_key TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    event_id TEXT NOT NULL,
    parent_scope_kind TEXT NOT NULL,
    parent_scope_id TEXT NOT NULL,
    asset_type TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    claimed_by TEXT NULL,
    claimed_at TIMESTAMPTZ NULL,
    claim_expires_at TIMESTAMPTZ NULL,
    claim_fencing_token BIGINT NOT NULL DEFAULT 0,
    handed_off_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    PRIMARY KEY (trigger_id)
);

-- These ALTERs backfill columns this EnsureSchema DDL added after the table
-- may already exist from a pre-#4576 deployment. CREATE TABLE IF NOT EXISTS
-- above is a no-op against an existing table, so without these, a store
-- created before #4576 would be missing claim_expires_at/claim_fencing_token
-- and the index/query below would fail at startup with "column ... does not
-- exist" (flagged in PR #4682 review).
ALTER TABLE gcp_freshness_triggers ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ NULL;
ALTER TABLE gcp_freshness_triggers ADD COLUMN IF NOT EXISTS claim_fencing_token BIGINT NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS gcp_freshness_triggers_freshness_key_idx
    ON gcp_freshness_triggers (freshness_key);

CREATE INDEX IF NOT EXISTS gcp_freshness_triggers_status_received_idx
    ON gcp_freshness_triggers (status, received_at ASC, trigger_id ASC);

CREATE INDEX IF NOT EXISTS gcp_freshness_triggers_delivery_key_idx
    ON gcp_freshness_triggers (delivery_key, updated_at DESC);

-- Reclaim index for the expired-claim-lease reap query (#4576): finds
-- 'claimed' rows whose lease has expired so they can be requeued rather than
-- stranded forever after a mid-batch handoff abort or coordinator crash.
CREATE INDEX IF NOT EXISTS gcp_freshness_triggers_claimed_lease_idx
    ON gcp_freshness_triggers (claim_expires_at)
    WHERE status = 'claimed';
`
