-- #10566: seed the advisory-group coalescing anchor.
--
-- Advisory_group_projection rows are re-homed to a synthetic long-lived
-- scope/generation owned by the supply-chain domain, so one live row per
-- group caps the backlog at the group count. The shape mirrors migration
-- 115 (the eshu:global value-flow singleton anchor): scope
-- `synthetic:advisory-groups` (kind `synthetic`, owned by no collector)
-- holds one perpetually-active generation
-- (`synthetic:advisory-groups:genesis`), and the claim-time supersede
-- sweep never fires on these rows by its existing guard (a row is a
-- candidate only when its generation is older than its scope's active
-- generation; the genesis is always the active one), while retention
-- only considers `superseded` generations (the genesis stays `active`).
--
-- The seed carries no work item: per-group slots arise from the first
-- touch of each group through the existing enqueue/retouch path, so the
-- domain row count stays bounded by the group count with no sweeper.
--
-- Reserved-id handling is fail-safe, never overwrite. Both inserts
-- are ON CONFLICT DO NOTHING: this directory has no applied-migration
-- ledger, so every file replays on every bootstrap and the seed must
-- converge once and no-op after it. A colliding scope id keeps its
-- existing row (DO NOTHING does not merge), and the seed test fails
-- loudly on the kind mismatch instead of silently adopting a foreign
-- scope.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'synthetic:advisory-groups', 'synthetic', 'synthetic', 'synthetic:advisory-groups', 'synthetic', 'synthetic:advisory-groups',
    clock_timestamp(), clock_timestamp(), 'active', 'synthetic:advisory-groups:genesis'
) ON CONFLICT (scope_id) DO NOTHING;

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    'synthetic:advisory-groups:genesis', 'synthetic:advisory-groups', 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), 'active'
) ON CONFLICT (generation_id) DO NOTHING;
