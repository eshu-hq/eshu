CREATE TABLE IF NOT EXISTS workflow_runs (
    run_id TEXT PRIMARY KEY,
    trigger_kind TEXT NOT NULL,
    status TEXT NOT NULL,
    requested_scope_set JSONB NOT NULL DEFAULT '[]'::jsonb,
    requested_collector TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS workflow_runs_status_updated_idx
    ON workflow_runs (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS workflow_work_items (
    work_item_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES workflow_runs(run_id) ON DELETE CASCADE,
    collector_kind TEXT NOT NULL,
    collector_instance_id TEXT NOT NULL,
    source_system TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fairness_key TEXT NULL,
    status TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    current_claim_id TEXT NULL,
    current_fencing_token BIGINT NOT NULL DEFAULT 0,
    current_owner_id TEXT NULL,
    lease_expires_at TIMESTAMPTZ NULL,
    visible_at TIMESTAMPTZ NULL,
    last_claimed_at TIMESTAMPTZ NULL,
    last_completed_at TIMESTAMPTZ NULL,
    last_failure_class TEXT NULL,
    last_failure_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS workflow_work_items_claimable_idx
    ON workflow_work_items (
        collector_kind,
        collector_instance_id,
        status,
        visible_at,
        updated_at DESC
    );
CREATE INDEX IF NOT EXISTS workflow_work_items_lease_idx
    ON workflow_work_items (lease_expires_at)
    WHERE lease_expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS workflow_work_items_run_idx
    ON workflow_work_items (run_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS workflow_work_items_registry_status_updated_idx
    ON workflow_work_items (collector_kind, status, updated_at DESC)
    WHERE collector_kind IN ('oci_registry', 'package_registry');
CREATE UNIQUE INDEX IF NOT EXISTS workflow_work_items_tfstate_candidate_nonterminal_idx
    ON workflow_work_items (
        collector_instance_id,
        scope_id,
        generation_id
    )
    WHERE collector_kind = 'terraform_state'
      AND generation_id LIKE 'terraform_state_candidate:%'
      AND status IN ('pending', 'claimed', 'failed_retryable', 'expired');

ALTER TABLE workflow_work_items
    ADD COLUMN IF NOT EXISTS source_system TEXT DEFAULT '';

ALTER TABLE workflow_work_items
    ADD COLUMN IF NOT EXISTS acceptance_unit_id TEXT DEFAULT '';

ALTER TABLE workflow_work_items
    ADD COLUMN IF NOT EXISTS source_run_id TEXT DEFAULT '';

ALTER TABLE workflow_work_items
    ALTER COLUMN source_system SET DEFAULT '',
    ALTER COLUMN acceptance_unit_id SET DEFAULT '',
    ALTER COLUMN source_run_id SET DEFAULT '';

UPDATE workflow_work_items
SET generation_id = work_item_id || ':legacy-missing-generation',
    status = CASE WHEN status IN ('pending', 'claimed') THEN 'failed_terminal' ELSE status END,
    current_claim_id = CASE WHEN status IN ('pending', 'claimed') THEN NULL ELSE current_claim_id END,
    current_owner_id = CASE WHEN status IN ('pending', 'claimed') THEN NULL ELSE current_owner_id END,
    lease_expires_at = CASE WHEN status IN ('pending', 'claimed') THEN NULL ELSE lease_expires_at END,
    last_failure_class = CASE
        WHEN status IN ('pending', 'claimed') THEN 'legacy_missing_generation_identity'
        ELSE last_failure_class
    END,
    last_failure_message = CASE
        WHEN status IN ('pending', 'claimed') THEN 'workflow work item predates required generation identity'
        ELSE last_failure_message
    END,
    updated_at = NOW()
WHERE generation_id IS NULL OR generation_id = '';

UPDATE workflow_work_items
SET source_system = collector_kind
WHERE source_system IS NULL OR source_system = '';

UPDATE workflow_work_items
SET acceptance_unit_id = scope_id
WHERE acceptance_unit_id IS NULL OR acceptance_unit_id = '';

UPDATE workflow_work_items
SET source_run_id = generation_id
WHERE source_run_id IS NULL OR source_run_id = '';

ALTER TABLE workflow_work_items
    ALTER COLUMN generation_id SET NOT NULL,
    ALTER COLUMN source_system SET NOT NULL,
    ALTER COLUMN acceptance_unit_id SET NOT NULL,
    ALTER COLUMN source_run_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS workflow_work_items_phase_tuple_idx
    ON workflow_work_items (run_id, scope_id, acceptance_unit_id, source_run_id, generation_id);

CREATE TABLE IF NOT EXISTS workflow_claims (
    claim_id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES workflow_work_items(work_item_id) ON DELETE CASCADE,
    fencing_token BIGINT NOT NULL,
    owner_id TEXT NOT NULL,
    status TEXT NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    heartbeat_at TIMESTAMPTZ NOT NULL,
    lease_expires_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (work_item_id, fencing_token)
);
CREATE INDEX IF NOT EXISTS workflow_claims_active_expiry_idx
    ON workflow_claims (status, lease_expires_at ASC);
CREATE INDEX IF NOT EXISTS workflow_claims_work_item_idx
    ON workflow_claims (work_item_id, updated_at DESC);
