CREATE TABLE IF NOT EXISTS webhook_refresh_triggers (
    trigger_id TEXT NOT NULL,
    delivery_key TEXT NOT NULL,
    refresh_key TEXT NOT NULL,
    provider TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    decision TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    delivery_id TEXT NOT NULL,
    repository_external_id TEXT NOT NULL,
    repository_full_name TEXT NOT NULL,
    default_branch TEXT NOT NULL,
    ref TEXT NOT NULL,
    before_sha TEXT NOT NULL DEFAULT '',
    target_sha TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT '',
    sender TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    received_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    claimed_by TEXT NULL,
    claimed_at TIMESTAMPTZ NULL,
    handed_off_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    PRIMARY KEY (trigger_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS webhook_refresh_triggers_refresh_key_idx
    ON webhook_refresh_triggers (refresh_key);

CREATE INDEX IF NOT EXISTS webhook_refresh_triggers_status_idx
    ON webhook_refresh_triggers (status, updated_at ASC);

CREATE INDEX IF NOT EXISTS webhook_refresh_triggers_delivery_key_idx
    ON webhook_refresh_triggers (delivery_key, updated_at DESC);
