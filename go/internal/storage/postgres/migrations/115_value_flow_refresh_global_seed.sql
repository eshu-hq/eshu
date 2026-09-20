-- #6785: seed the value-flow refresh singleton anchor.
--
-- The refresh re-runs a GLOBAL fixpoint, so its consumer is one work item at
-- a fixed singleton identity instead of per-repo rows: scope `eshu:global`
-- (kind `global`) holds one perpetually-active generation
-- (`eshu:global:genesis`) plus the `code_value_flow_refresh` item. The
-- existing fanout `current_consumers` join matches the singleton via
-- active_generation_id, so producer completions in ANY later generation
-- reopen it -- the later-generation re-enqueue falls out of the existing
-- join, with no fanout SQL change.
--
-- The generation never supersedes: no collector syncs scope `eshu:global`,
-- and retention prunes only superseded non-active generations. The item seeds
-- `succeeded` so a fresh bootstrap does not immediately claim a global solve;
-- the first producer completion reopens it. Its conflict key is the scope
-- itself, so concurrent refresh runs serialize on the global key.
--
-- All three inserts are ON CONFLICT DO NOTHING: this directory has no
-- applied-migration ledger, so every file replays on every bootstrap and the
-- seed must converge once and no-op after it.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'eshu:global', 'global', 'eshu', 'eshu:global', 'reducer', 'eshu:global',
    clock_timestamp(), clock_timestamp(), 'active', 'eshu:global:genesis'
) ON CONFLICT (scope_id) DO NOTHING;

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    'eshu:global:genesis', 'eshu:global', 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), 'active'
) ON CONFLICT (generation_id) DO NOTHING;

INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    payload, created_at, updated_at
) VALUES (
    'reducer_eshu_global_code_value_flow_refresh', 'eshu:global', 'eshu:global:genesis',
    'reducer', 'code_value_flow_refresh',
    'scope', 'eshu:global', 'succeeded', 0,
    jsonb_build_object(
        'entity_key', 'code_value_flow_refresh:global',
        'reason', 'value-flow refresh singleton',
        'source_system', 'reducer'
    ),
    clock_timestamp(), clock_timestamp()
) ON CONFLICT (work_item_id) DO NOTHING;
