CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_materialization (
    singleton SMALLINT PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    materialized_at TIMESTAMPTZ NOT NULL
);
