CREATE TABLE IF NOT EXISTS collector_generation_dead_letters (
    generation_id TEXT NOT NULL PRIMARY KEY,
    scope_id TEXT NOT NULL,
    collector_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    scope_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    trigger_kind TEXT NOT NULL,
    payload_reference JSONB NOT NULL DEFAULT '{}'::jsonb,
    failure_class TEXT NOT NULL,
    failure_message TEXT NOT NULL,
    status TEXT NOT NULL,
    replay_count INTEGER NOT NULL DEFAULT 0,
    first_dead_lettered_at TIMESTAMPTZ NOT NULL,
    last_dead_lettered_at TIMESTAMPTZ NOT NULL,
    last_replay_requested_at TIMESTAMPTZ NULL,
    last_replayed_at TIMESTAMPTZ NULL,
    replayed_generation_id TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE collector_generation_dead_letters
    ADD COLUMN IF NOT EXISTS last_replayed_at TIMESTAMPTZ NULL;

ALTER TABLE collector_generation_dead_letters
    ADD COLUMN IF NOT EXISTS replayed_generation_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS collector_generation_dead_letters_status_idx
    ON collector_generation_dead_letters (status, last_dead_lettered_at ASC, generation_id ASC);

CREATE INDEX IF NOT EXISTS collector_generation_dead_letters_scope_idx
    ON collector_generation_dead_letters (scope_id, status, last_dead_lettered_at ASC);

CREATE INDEX IF NOT EXISTS collector_generation_dead_letters_collector_idx
    ON collector_generation_dead_letters (collector_kind, status, last_dead_lettered_at ASC);
