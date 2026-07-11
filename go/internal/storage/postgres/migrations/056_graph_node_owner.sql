-- #5007 Stage 1: deterministic cross-scope owner ledger.
--
-- When two ingestion scopes carry the same resource identity, both scopes'
-- reducer intents project the same canonical node uid and race to write its
-- scope-derived properties. NornicDB does not reliably detect concurrent
-- property-write conflicts on a shared existing node (#5062), so the graph
-- alone cannot resolve the winner deterministically. This table is the
-- authoritative, Postgres-atomic resolver: for each node uid it records the
-- current max (observed_at, source_fact_id) order key. Under a per-uid advisory
-- lock, each reducer node-write batch upserts its contribution here (the
-- ON CONFLICT ... WHERE excluded.source_order_key > graph_node_owner.
-- source_order_key clause atomically keeps the max), then writes ONLY the uids
-- it currently owns to the graph, so the graph node's scope-derived truth is a
-- deterministic function of the SET of contributing facts, independent of
-- commit order or reducer worker count.
--
-- Keyed on uid alone because canonical node uids are globally unique across
-- labels (facts.StableID folds the label/resource_type into the uid), so one
-- table serves every owning family (CloudResource for AWS/GCP/Azure and the
-- EC2 instance node, plus KubernetesWorkload). winning_row holds the winning
-- contributor's full node row as JSONB; Stage 1 does not read it back for the
-- graph write (the current-max writer writes its own Go-typed row to preserve
-- byte-identity), but it is the durable foundation for the Stage 2 per-scope
-- provenance satellites. The uid primary key is the only access path (upsert
-- ON CONFLICT (uid) and the batched winner read-back WHERE uid = ANY($1) both
-- resolve on it), so no secondary index is added.
CREATE TABLE IF NOT EXISTS graph_node_owner (
    uid TEXT PRIMARY KEY,
    source_order_key TEXT NOT NULL,
    winning_row JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
