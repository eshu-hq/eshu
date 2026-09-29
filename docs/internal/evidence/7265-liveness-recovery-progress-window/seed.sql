-- #7265 liveness progress-window synthetic dataset.
-- Apply after the production DDL: explain.sh schema applies migrations 001,
-- 002, 005, 008, 043, 091, 108 and 113 from
-- go/internal/storage/postgres/migrations/.
-- 1000 aged active generations + 200 fresh; 300K pending intents; 1M completed
-- intents; four projection domains. All timestamps are relative to now(), so
-- measure right after seeding: the exact repo_dependency completions age out of
-- the 10-minute window over time.
-- Doubled-window variant (250K in-window completions): explain.sh seed-doubled
-- rewrites "(i % 1200) * interval '1 second'" to "(i % 300) * interval '1 second'".
--   gens    1..400  pending in code_calls         (domain progressing: completions within window)
--   gens  401..700  pending in sql_relationships  (domain quiet: newest completion 2h ago)
--   gens  701..850  pending in inheritance_edges  (domain progressing)
--   gens  851..980  pending only in exact repo_dependency family (not actionable)
--   gens  981..1000 pending in repo_dependency lookalike source runs (actionable),
--                   domain whose window completions are ~all exact family
--   every 10th gen also carries one quiet sql_relationships intent (mixed domains)
BEGIN;
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id)
SELECT 'scope-' || i, 'repository', 'github', 'acme/r' || i, 'git', 'acme/r' || i,
       now(), now(), 'active', 'gen-' || i
FROM generate_series(1, 1200) AS i;

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen-' || i, 'scope-' || i, 'push', now() - interval '2 hours', now() - interval '2 hours', 'active',
       CASE WHEN i <= 1000 THEN now() - interval '2 hours' ELSE now() - interval '5 minutes' END
FROM generate_series(1, 1200) AS i;

-- 20K historical superseded generations.
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, superseded_at)
SELECT 'gen-old-' || i, 'scope-' || ((i % 1200) + 1), 'push', now() - interval '3 days', now() - interval '3 days',
       'superseded', now() - interval '3 days', now() - interval '2 days'
FROM generate_series(1, 20000) AS i;

-- Succeeded source_local projector row per active generation.
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at)
SELECT 'projector_scope-' || i || '_gen-' || i, 'scope-' || i, 'gen-' || i, 'projector', 'source_local', 'succeeded',
       '{}'::jsonb, now() - interval '2 hours', now() - interval '2 hours'
FROM generate_series(1, 1200) AS i;
-- Five succeeded reducer rows per active generation.
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at)
SELECT 'reducer_' || d || '_gen-' || i, 'scope-' || i, 'gen-' || i, 'reducer', 'domain_' || d, 'succeeded',
       '{}'::jsonb, now() - interval '2 hours', now() - interval '100 minutes'
FROM generate_series(1, 1200) AS i, generate_series(1, 5) AS d;
-- 200K historical work items for superseded generations.
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at)
SELECT 'hist_' || i, 'scope-' || (((i % 20000) + 1) % 1200 + 1), 'gen-old-' || ((i % 20000) + 1),
       CASE WHEN i % 2 = 0 THEN 'reducer' ELSE 'projector' END, 'source_local', 'succeeded',
       '{}'::jsonb, now() - interval '3 days', now() - interval '3 days'
FROM generate_series(1, 200000) AS i;

-- 300K pending intents.
INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
    repository_id, source_run_id, generation_id, partition_hash, payload, created_at)
SELECT 'pending-' || i,
       CASE
           WHEN g <= 400 THEN 'code_calls'
           WHEN g <= 700 THEN 'sql_relationships'
           WHEN g <= 850 THEN 'inheritance_edges'
           ELSE 'repo_dependency'
       END,
       'p' || (i % 997), 'scope-' || g, 'scope-' || g, 'acme/r' || g,
       CASE
           WHEN g <= 850 THEN 'run-' || g
           WHEN g <= 980 THEN 'repo_dependency:scope-' || g
           ELSE 'code_import_repo_dependency:scope-' || g
       END,
       'gen-' || g, (i % 1000)::numeric, '{"action":"upsert"}'::jsonb, now() - interval '90 minutes'
FROM (SELECT i, (i % 1000) + 1 AS g FROM generate_series(1, 300000) AS i) AS s;
-- Mixed-domain intents: every 10th aged gen also has one quiet-domain intent.
INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
    repository_id, source_run_id, generation_id, partition_hash, payload, created_at)
SELECT 'pending-mixed-' || g, 'sql_relationships', 'pm' || g, 'scope-' || g, 'scope-' || g, 'acme/r' || g,
       'run-' || g, 'gen-' || g, 1, '{"action":"upsert"}'::jsonb, now() - interval '90 minutes'
FROM generate_series(10, 1000, 10) AS g;

-- 1M completed intents across all four domains (any generation).
INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
    repository_id, source_run_id, generation_id, partition_hash, payload, created_at, completed_at)
SELECT 'done-' || i, dom, 'p' || (i % 997), 'scope-' || g, 'scope-' || g, 'acme/r' || g,
       CASE WHEN dom = 'repo_dependency' THEN 'repo_dependency:scope-' || g ELSE 'run-' || g END,
       CASE WHEN i % 3 = 0 THEN 'gen-' || g ELSE 'gen-old-' || ((i % 20000) + 1) END,
       (i % 1000)::numeric, '{"action":"upsert"}'::jsonb, done_at - interval '1 minute', done_at
FROM (
    SELECT i, (i % 1200) + 1 AS g,
           CASE i % 4
               WHEN 0 THEN 'code_calls'
               WHEN 1 THEN 'sql_relationships'
               WHEN 2 THEN 'inheritance_edges'
               ELSE 'repo_dependency'
           END AS dom,
           CASE i % 4
               -- quiet domain: newest completion 2 hours ago
               WHEN 1 THEN now() - interval '2 hours' - (i % 7200) * interval '1 minute'
               -- exact repo_dependency: dense recent completions (half within 10m)
               WHEN 3 THEN now() - (i % 1200) * interval '1 second'
               -- progressing domains: spread over the last 5 days incl. last minutes
               ELSE now() - (i % 7200) * interval '1 minute'
           END AS done_at
    FROM generate_series(1, 1000000) AS i
) AS s;
COMMIT;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;
ANALYZE fact_work_items;
ANALYZE shared_projection_intents;
