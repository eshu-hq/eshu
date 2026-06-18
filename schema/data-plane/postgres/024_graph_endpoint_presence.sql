CREATE TABLE IF NOT EXISTS graph_endpoint_presence (
    keyspace TEXT NOT NULL,
    uid TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    repo_id TEXT NOT NULL DEFAULT '',
    source_generation TEXT NOT NULL DEFAULT '',
    committed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (keyspace, uid)
);
ALTER TABLE graph_endpoint_presence
    ADD COLUMN IF NOT EXISTS repo_id TEXT NOT NULL DEFAULT '';
ALTER TABLE graph_endpoint_presence
    ADD COLUMN IF NOT EXISTS source_generation TEXT NOT NULL DEFAULT '';
-- One-time, NON-DESTRUCTIVE backfill of pre-#2842 repo_workload presence rows. On
-- an upgraded database the ADD COLUMN ... DEFAULT '' above leaves legacy
-- api_endpoint_repo_path / repo_workload rows with a blank repo_id, which the
-- repo-scoped stale-row delete (keyed repo_id = ANY(...)) can never match. For the
-- repo_workload keyspace the uid IS the bare repo_id, so we recover it here. We do
-- NOT delete: deleting a blank-provenance row would remove still-CURRENT target
-- presence too, and because filterRowsByReadiness terminalizes (does not defer) a
-- handles_route/runs_in row whose presence is absent, that would silently drop a
-- live edge until the next re-materialization (#2842/#2903 review). After this
-- backfill, the runtime retract (RetractStaleRepoGenerations, #2896 path that
-- covers every scope repo) deletes the legacy row only when its repo
-- re-materializes with a different generation, while a still-current row is
-- re-upserted with the live generation first and survives. The api_endpoint_repo_path
-- uid is a SHA-256 hash (#2844), so repo_id is unrecoverable there; those legacy
-- rows are left in place and are bounded-safe — the HANDLES_ROUTE MERGE re-MATCHes
-- the actual :Endpoint node, so a stale-present row never creates an edge to a
-- removed or re-pathed endpoint, and a current endpoint re-upserts proper
-- provenance on its next materialization. The predicate matches nothing once
-- migrated, so it is idempotent on every EnsureSchema.
UPDATE graph_endpoint_presence
SET repo_id = uid
WHERE keyspace = 'repo_workload'
  AND repo_id = ''
  AND uid <> '';
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_scope_idx
    ON graph_endpoint_presence (scope_id);
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_updated_idx
    ON graph_endpoint_presence (updated_at DESC);
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_stale_idx
    ON graph_endpoint_presence (keyspace, scope_id, repo_id);
