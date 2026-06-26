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
