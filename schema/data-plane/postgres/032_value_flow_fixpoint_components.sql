CREATE TABLE IF NOT EXISTS value_flow_fixpoint_components (
    component_key TEXT PRIMARY KEY,
    result JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS value_flow_fixpoint_components_updated_idx
    ON value_flow_fixpoint_components (updated_at DESC, component_key);
