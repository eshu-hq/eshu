// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const generationRetentionEventSchemaSQL = `
CREATE TABLE IF NOT EXISTS generation_retention_events (
    event_id TEXT PRIMARY KEY,
    scope_id_hash TEXT NOT NULL,
    generation_id_hash TEXT NOT NULL,
    scope_class TEXT NOT NULL,
    policy_scope TEXT NOT NULL,
    policy_revision TEXT NOT NULL,
    generation_observed_at TIMESTAMPTZ NULL,
    generation_superseded_at TIMESTAMPTZ NULL,
    reason TEXT NOT NULL,
    row_counts JSONB NOT NULL DEFAULT '{}'::jsonb,
    pruned_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS generation_retention_events_scope_idx
    ON generation_retention_events (scope_id_hash, generation_observed_at DESC);

CREATE INDEX IF NOT EXISTS generation_retention_events_generation_idx
    ON generation_retention_events (scope_id_hash, generation_id_hash);

CREATE INDEX IF NOT EXISTS generation_retention_events_reason_idx
    ON generation_retention_events (reason, pruned_at DESC);
`
