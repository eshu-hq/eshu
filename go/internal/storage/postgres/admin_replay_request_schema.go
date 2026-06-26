// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// adminReplayRequestSchemaSQL defines the durable idempotency ledger for
// operator replay requests. The idempotency_key primary key is the
// serialization point: under concurrent duplicate delivery, exactly one request
// wins the INSERT and executes the replay; every other request observes the
// existing row and returns the recorded outcome instead of replaying again.
const adminReplayRequestSchemaSQL = `
CREATE TABLE IF NOT EXISTS admin_replay_requests (
    idempotency_key TEXT NOT NULL PRIMARY KEY,
    request_fingerprint TEXT NOT NULL,
    status TEXT NOT NULL,
    reason_code TEXT NOT NULL DEFAULT '',
    replayed_count INTEGER NOT NULL DEFAULT 0,
    work_item_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS admin_replay_requests_status_idx
    ON admin_replay_requests (status, created_at DESC);
`

func adminReplayRequestBootstrapDefinition() Definition {
	return Definition{
		Name: "admin_replay_requests",
		Path: "schema/data-plane/postgres/031_admin_replay_requests.sql",
		SQL:  adminReplayRequestSchemaSQL,
	}
}
