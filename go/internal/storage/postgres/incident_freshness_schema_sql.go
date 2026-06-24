// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const incidentFreshnessSchemaSQL = `
CREATE TABLE IF NOT EXISTS incident_freshness_triggers (
    trigger_id TEXT NOT NULL,
    delivery_key TEXT NOT NULL,
    freshness_key TEXT NOT NULL,
    provider TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    event_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    claimed_by TEXT NULL,
    claimed_at TIMESTAMPTZ NULL,
    handed_off_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    PRIMARY KEY (trigger_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS incident_freshness_triggers_freshness_key_idx
    ON incident_freshness_triggers (freshness_key);

CREATE INDEX IF NOT EXISTS incident_freshness_triggers_status_received_idx
    ON incident_freshness_triggers (status, received_at ASC, trigger_id ASC);

CREATE INDEX IF NOT EXISTS incident_freshness_triggers_delivery_key_idx
    ON incident_freshness_triggers (delivery_key, updated_at DESC);
`
