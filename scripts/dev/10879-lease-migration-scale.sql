INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, lease_owner, claim_until,
    last_attempt_at, payload, created_at, updated_at
)
SELECT 'migproof-scale-' || g.i::text,
    'scope-migproof-1', 'generation-migproof-1', 'reducer', 'supply_chain_impact',
    'scope', CASE WHEN g.i % 3 = 0 THEN 'slive-key-' || g.i::text
        WHEN g.i % 7 = 0 THEN 'skey-' || (g.i % 997)::text ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN 'claimed' WHEN g.i % 3 = 1 THEN 'succeeded' ELSE 'pending' END,
    1,
    CASE WHEN g.i % 3 = 0 THEN 'sowner-' || (g.i % 11)::text ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN now() + interval '5 minutes' ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN now() ELSE NULL END,
    '{}'::jsonb, now(), now()
FROM generate_series(1, 2381103) AS g(i);
