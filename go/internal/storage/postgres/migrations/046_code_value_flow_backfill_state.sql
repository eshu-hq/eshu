CREATE TABLE IF NOT EXISTS code_value_flow_backfill_state (
    backfill_key TEXT PRIMARY KEY,
    completed_at TIMESTAMPTZ NOT NULL
);
