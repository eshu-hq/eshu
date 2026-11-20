INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ('scope-migproof-1', 'repository', 'git', 'migproof-repo-1', NULL,
    'git', 'migproof-repo-1', now(), now(), 'active', 'generation-migproof-1', '{}'::jsonb);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ('generation-migproof-1', 'scope-migproof-1', 'snapshot', 'migproof',
    now(), now(), 'active', now(), NULL, '{}'::jsonb);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, lease_owner, claim_until,
    last_attempt_at, payload, created_at, updated_at
)
SELECT 'migproof-work-' || g.i::text,
    'scope-migproof-1', 'generation-migproof-1', 'reducer', 'supply_chain_impact',
    'scope', CASE WHEN g.i % 3 = 0 THEN 'live-key-' || g.i::text
        WHEN g.i % 7 = 0 THEN 'key-' || (g.i % 997)::text ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN 'claimed' WHEN g.i % 3 = 1 THEN 'succeeded' ELSE 'pending' END,
    1,
    CASE WHEN g.i % 3 = 0 THEN 'owner-' || (g.i % 11)::text ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN now() + interval '5 minutes' ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN now() ELSE NULL END,
    '{}'::jsonb, now(), now()
FROM generate_series(1, 264567) AS g(i);
