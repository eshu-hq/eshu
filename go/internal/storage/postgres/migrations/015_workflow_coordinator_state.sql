CREATE TABLE IF NOT EXISTS collector_instances (
    instance_id TEXT PRIMARY KEY,
    collector_kind TEXT NOT NULL,
    mode TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    bootstrap BOOLEAN NOT NULL DEFAULT FALSE,
    claims_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    display_name TEXT NULL,
    configuration JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_observed_at TIMESTAMPTZ NOT NULL,
    deactivated_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS collector_instances_kind_enabled_idx
    ON collector_instances (collector_kind, enabled, mode, updated_at DESC);

CREATE TABLE IF NOT EXISTS workflow_run_completeness (
    run_id TEXT NOT NULL REFERENCES workflow_runs(run_id) ON DELETE CASCADE,
    collector_kind TEXT NOT NULL,
    keyspace TEXT NOT NULL DEFAULT 'code_entities_uid',
    phase_name TEXT NOT NULL,
    required BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL,
    detail TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (run_id, collector_kind, keyspace, phase_name)
);
ALTER TABLE workflow_run_completeness
    ADD COLUMN IF NOT EXISTS keyspace TEXT;
UPDATE workflow_run_completeness
SET keyspace = 'code_entities_uid'
WHERE keyspace IS NULL OR BTRIM(keyspace) = '';
ALTER TABLE workflow_run_completeness
    ALTER COLUMN keyspace SET DEFAULT 'code_entities_uid';
ALTER TABLE workflow_run_completeness
    ALTER COLUMN keyspace SET NOT NULL;
DO $workflow_run_completeness$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'workflow_run_completeness_pkey'
          AND conrelid = 'workflow_run_completeness'::regclass
    ) THEN
        ALTER TABLE workflow_run_completeness
            DROP CONSTRAINT workflow_run_completeness_pkey;
    END IF;
    ALTER TABLE workflow_run_completeness
        ADD CONSTRAINT workflow_run_completeness_pkey
        PRIMARY KEY (run_id, collector_kind, keyspace, phase_name);
END
$workflow_run_completeness$;
CREATE INDEX IF NOT EXISTS workflow_run_completeness_status_idx
    ON workflow_run_completeness (status, updated_at DESC);
