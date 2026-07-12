INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES (
    'scope-incremental-refresh', 'repository', 'git', 'repo-incremental-refresh',
    NULL, 'git', 'repo-incremental-refresh', TIMESTAMPTZ '2026-04-16T00:00:00Z',
    TIMESTAMPTZ '2026-04-16T00:05:00Z', 'pending', NULL,
    $json${"repo_id":"repo-123"}$json$::jsonb
)
ON CONFLICT (scope_id) DO UPDATE SET
    scope_kind = EXCLUDED.scope_kind,
    source_system = EXCLUDED.source_system,
    source_key = EXCLUDED.source_key,
    parent_scope_id = EXCLUDED.parent_scope_id,
    collector_kind = EXCLUDED.collector_kind,
    partition_key = EXCLUDED.partition_key,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = EXCLUDED.status,
    active_generation_id = EXCLUDED.active_generation_id,
    payload = EXCLUDED.payload;

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint,
    observed_at, ingested_at, status, activated_at, superseded_at, payload
) VALUES
(
    'generation-incremental-refresh-a', 'scope-incremental-refresh', 'snapshot',
    'fingerprint-a', TIMESTAMPTZ '2026-04-16T00:00:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z',
    'pending', NULL, NULL, '{}'::jsonb
),
(
    'generation-incremental-refresh-b', 'scope-incremental-refresh', 'snapshot',
    'fingerprint-b', TIMESTAMPTZ '2026-04-16T00:01:00Z', TIMESTAMPTZ '2026-04-16T00:06:00Z',
    'pending', NULL, NULL, '{}'::jsonb
)
ON CONFLICT (generation_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    trigger_kind = EXCLUDED.trigger_kind,
    freshness_hint = EXCLUDED.freshness_hint,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = EXCLUDED.status,
    activated_at = EXCLUDED.activated_at,
    superseded_at = EXCLUDED.superseded_at,
    payload = EXCLUDED.payload;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, source_uri, source_record_id,
    observed_at, ingested_at, is_tombstone, payload
) VALUES
(
    'fact-incremental-refresh-a', 'scope-incremental-refresh', 'generation-incremental-refresh-a',
    'repository', 'repository:fact-incremental-refresh-a', 'git', 'fact-incremental-refresh-a',
    NULL, NULL, TIMESTAMPTZ '2026-04-16T00:00:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z',
    FALSE, $json${"graph_id":"incremental-refresh-proof-repo","graph_kind":"repository","name":"eshu","content_path":"README.md","content_body":"initial body","content_digest":"initial body"}$json$::jsonb
),
(
    'fact-incremental-refresh-b', 'scope-incremental-refresh', 'generation-incremental-refresh-b',
    'repository', 'repository:fact-incremental-refresh-b', 'git', 'fact-incremental-refresh-b',
    NULL, NULL, TIMESTAMPTZ '2026-04-16T00:01:00Z', TIMESTAMPTZ '2026-04-16T00:06:00Z',
    FALSE, $json${"graph_id":"incremental-refresh-proof-repo","graph_kind":"repository","name":"eshu","content_path":"README.md","content_body":"changed body","content_digest":"changed body"}$json$::jsonb
)
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind = EXCLUDED.fact_kind,
    stable_fact_key = EXCLUDED.stable_fact_key,
    source_system = EXCLUDED.source_system,
    source_fact_key = EXCLUDED.source_fact_key,
    source_uri = EXCLUDED.source_uri,
    source_record_id = EXCLUDED.source_record_id,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    is_tombstone = EXCLUDED.is_tombstone,
    payload = EXCLUDED.payload;

INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at,
    last_attempt_at, next_attempt_at, failure_class, failure_message,
    failure_details, payload, created_at, updated_at
) VALUES
(
    'projector_scope-incremental-refresh_generation-incremental-refresh-a',
    'scope-incremental-refresh', 'generation-incremental-refresh-a', 'projector',
    'source_local', 'pending', 0, NULL, NULL, NULL, NULL, NULL, NULL, NULL,
    NULL, '{}'::jsonb, TIMESTAMPTZ '2026-04-16T00:05:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z'
),
(
    'projector_scope-incremental-refresh_generation-incremental-refresh-b',
    'scope-incremental-refresh', 'generation-incremental-refresh-b', 'projector',
    'source_local', 'pending', 0, NULL, NULL, NULL, NULL, NULL, NULL, NULL,
    NULL, '{}'::jsonb, TIMESTAMPTZ '2026-04-16T00:06:00Z', TIMESTAMPTZ '2026-04-16T00:06:00Z'
)
ON CONFLICT (work_item_id) DO NOTHING;
