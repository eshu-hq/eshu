// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// generationLivenessRecoveryInFlightOnlySeedSQL seeds only a pending liveness
// re-drive row. Use this when the test must prove an already-pending recovery
// is not re-selected even when the remaining attempt budget is not exhausted.
const generationLivenessRecoveryInFlightOnlySeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-recovery-inflight', 'repository', 'github', 'acme/recovery-inflight', 'git',
    'acme/recovery-inflight', now(), now(), 'active', 'gen-recovery-inflight'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-recovery-inflight', 'scope-recovery-inflight', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-recovery-inflight', 'repo_dependency', 'acme/recovery-inflight', 'scope-recovery-inflight',
    '', 'acme/recovery-inflight', 'run-recovery-inflight', 'gen-recovery-inflight',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at
) VALUES (
    'projector_scope-recovery-inflight_gen-recovery-inflight',
    'scope-recovery-inflight', 'gen-recovery-inflight',
    'projector', 'source_local', 'pending',
    '{"liveness_recovery_attempts":1}'::jsonb,
    now() - interval '1 minute', now() - interval '1 minute'
);
`
