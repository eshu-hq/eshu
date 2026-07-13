// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const generationLivenessSharedBacklogSeedSQL = `
-- Cross-repo repo_dependency remains owned by the shared resolver after
-- backward_evidence_committed. Liveness must not reopen source_local while the
-- separate shared queue drains.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-shared-backlog', 'repository', 'github', 'acme/shared-backlog', 'git',
    'acme/shared-backlog', now(), now(), 'active', 'gen-shared-backlog'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-shared-backlog', 'scope-shared-backlog', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-shared-backlog', 'repo_dependency', 'acme/shared-backlog', 'scope-shared-backlog',
    '', 'acme/shared-backlog', 'repo_dependency:scope-shared-backlog', 'gen-shared-backlog',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
INSERT INTO graph_projection_phase_state (
    scope_id, acceptance_unit_id, source_run_id, generation_id,
    keyspace, phase, committed_at, updated_at
) VALUES (
    'scope-shared-backlog', 'scope-shared-backlog',
    'gen-shared-backlog', 'gen-shared-backlog',
    'cross_repo_evidence', 'backward_evidence_committed',
    now() - interval '90 minutes', now() - interval '90 minutes'
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at
) VALUES
    (
        'projector_scope-shared-backlog_gen-shared-backlog',
        'scope-shared-backlog', 'gen-shared-backlog',
        'projector', 'source_local', 'succeeded', '{}'::jsonb,
        now() - interval '2 hours', now() - interval '2 hours'
    ),
    (
        'reducer_gen-shared-backlog',
        'scope-shared-backlog', 'gen-shared-backlog',
        'reducer', 'repo_dependency', 'succeeded', '{}'::jsonb,
        now() - interval '2 hours', now() - interval '90 minutes'
    );
`
