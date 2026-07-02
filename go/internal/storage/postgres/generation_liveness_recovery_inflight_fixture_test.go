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

// generationLivenessExpiredLeaseOnlySeedSQL seeds a scope whose source-local
// projector work item is stuck in 'claimed' with a lease that expired long
// ago (claim_until in the past). This reproduces the #4464 Bug 2 wedge: the
// sole claimer of source_local work (a one-shot bootstrap-index) died holding
// the claim, and nothing else in the runtime topology ever calls Claim()
// again for source_local (the continuous ingester's
// depends_on: bootstrap-index: condition: service_completed_successfully
// gate means ingester never starts after a bootstrap-index crash). Before the
// fix, this row looked "in flight" to the liveness sweep forever, even though
// its lease expired 25 minutes ago; the generation was permanently wedged
// with zero dead-letters. The fix must re-drive it because the lease is
// expired, while a *live* lease (claim_until in the future, exercised by
// RecoveryInFlightNoOp's pending-status sibling) must still block re-drive.
const generationLivenessExpiredLeaseOnlySeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-expired-lease', 'repository', 'github', 'acme/expired-lease', 'git',
    'acme/expired-lease', now(), now(), 'active', 'gen-expired-lease'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-expired-lease', 'scope-expired-lease', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-expired-lease', 'graph', 'acme/expired-lease', 'scope-expired-lease',
    '', 'acme/expired-lease', 'run-expired-lease', 'gen-expired-lease',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    lease_owner, claim_until, payload, created_at, updated_at
) VALUES (
    'projector_scope-expired-lease_gen-expired-lease',
    'scope-expired-lease', 'gen-expired-lease',
    'projector', 'source_local', 'claimed',
    'bootstrap-index', now() - interval '25 minutes',
    '{}'::jsonb,
    now() - interval '31 minutes', now() - interval '31 minutes'
);
`

// generationLivenessLiveLeaseOnlySeedSQL is the negative-case sibling of
// generationLivenessExpiredLeaseOnlySeedSQL: the source-local projector work
// item is 'running' with a lease that has NOT yet expired (claim_until 4
// minutes in the future, well inside a normal projector heartbeat interval).
// This scope must NOT be re-driven — a live lease means a worker is
// genuinely, currently projecting it. This proves the #4464 Bug 2 fix
// discriminates on lease expiry, not merely on status, so it cannot
// re-introduce the shared-context cascade by racing a live worker.
const generationLivenessLiveLeaseOnlySeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-live-lease', 'repository', 'github', 'acme/live-lease', 'git',
    'acme/live-lease', now(), now(), 'active', 'gen-live-lease'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-live-lease', 'scope-live-lease', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-live-lease', 'graph', 'acme/live-lease', 'scope-live-lease',
    '', 'acme/live-lease', 'run-live-lease', 'gen-live-lease',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    lease_owner, claim_until, payload, created_at, updated_at
) VALUES (
    'projector_scope-live-lease_gen-live-lease',
    'scope-live-lease', 'gen-live-lease',
    'projector', 'source_local', 'running',
    'ingester', now() + interval '4 minutes',
    '{}'::jsonb,
    now() - interval '1 minute', now() - interval '1 minute'
);
`
