CREATE TABLE IF NOT EXISTS search_vector_build_materialization (
    singleton SMALLINT PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    materialized_at TIMESTAMPTZ NOT NULL
);
