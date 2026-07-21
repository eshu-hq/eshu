-- One-time completion markers for owner-ledger upgrade backfills.
--
-- The cloud-resource list read path pages from graph_node_owner. Deployments
-- that already have CloudResource graph nodes must seed those rows before the
-- indexed read path is exposed. A durable marker prevents every API or MCP
-- restart from rescanning the graph; it is written only after every page has
-- committed, so a partial failure retries safely on the next startup.
CREATE TABLE IF NOT EXISTS graph_node_owner_backfill_state (
    backfill_key TEXT PRIMARY KEY,
    completed_at TIMESTAMPTZ NOT NULL
);
