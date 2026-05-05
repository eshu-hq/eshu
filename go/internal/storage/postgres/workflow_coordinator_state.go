package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const workflowCoordinatorStateSchemaSQL = `
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
`

const upsertCollectorInstanceQuery = `
INSERT INTO collector_instances (
    instance_id,
    collector_kind,
    mode,
    enabled,
    bootstrap,
    claims_enabled,
    display_name,
    configuration,
    last_observed_at,
    deactivated_at,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8::jsonb, $9::timestamptz,
    CASE WHEN $4 THEN NULL ELSE $9::timestamptz END,
    $9::timestamptz, $9::timestamptz
)
ON CONFLICT (instance_id) DO UPDATE
SET collector_kind = EXCLUDED.collector_kind,
    mode = EXCLUDED.mode,
    enabled = EXCLUDED.enabled,
    bootstrap = EXCLUDED.bootstrap,
    claims_enabled = EXCLUDED.claims_enabled,
    display_name = EXCLUDED.display_name,
    configuration = EXCLUDED.configuration,
    last_observed_at = EXCLUDED.last_observed_at,
    deactivated_at = EXCLUDED.deactivated_at,
    updated_at = EXCLUDED.updated_at
`

const deactivateStaleCollectorInstancesQuery = `
UPDATE collector_instances
SET enabled = FALSE,
    claims_enabled = FALSE,
    deactivated_at = $1,
    updated_at = $1
WHERE NOT (instance_id = ANY($2::text[]))
  AND (enabled = TRUE OR deactivated_at IS NULL)
`

const listCollectorInstancesQuery = `
SELECT
    instance_id,
    collector_kind,
    mode,
    enabled,
    bootstrap,
    claims_enabled,
    COALESCE(display_name, ''),
    configuration::text,
    last_observed_at,
    deactivated_at,
    created_at,
    updated_at
FROM collector_instances
ORDER BY collector_kind ASC, instance_id ASC
`

const upsertWorkflowRunCompletenessQuery = `
INSERT INTO workflow_run_completeness (
    run_id,
    collector_kind,
    keyspace,
    phase_name,
    required,
    status,
    detail,
    observed_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9)
ON CONFLICT (run_id, collector_kind, keyspace, phase_name) DO UPDATE
SET required = EXCLUDED.required,
    status = EXCLUDED.status,
    detail = EXCLUDED.detail,
    observed_at = EXCLUDED.observed_at,
    updated_at = EXCLUDED.updated_at
`

// WorkflowCoordinatorStateSchemaSQL returns the DDL for coordinator-owned
// instance and completeness state.
func WorkflowCoordinatorStateSchemaSQL() string {
	return workflowCoordinatorStateSchemaSQL
}

// ReconcileCollectorInstances applies declarative desired collector instances
// into durable coordinator state.
func (s *WorkflowControlStore) ReconcileCollectorInstances(
	ctx context.Context,
	observedAt time.Time,
	desired []workflow.DesiredCollectorInstance,
) error {
	if s.db == nil {
		return fmt.Errorf("workflow control store database is required")
	}
	for _, instance := range desired {
		if err := instance.Validate(); err != nil {
			return fmt.Errorf("reconcile collector instances: %w", err)
		}
	}

	execTarget := s.db
	commit := func() error { return nil }
	rollback := func() error { return nil }
	if s.beginner != nil {
		tx, err := s.beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("reconcile collector instances: begin transaction: %w", err)
		}
		execTarget = tx
		commit = tx.Commit
		rollback = tx.Rollback
	}
	defer func() { _ = rollback() }()

	now := observedAt.UTC()
	ids := make([]string, 0, len(desired))
	for _, instance := range desired {
		materialized := instance.Materialize(now)
		if _, err := execTarget.ExecContext(
			ctx,
			upsertCollectorInstanceQuery,
			materialized.InstanceID,
			string(materialized.CollectorKind),
			string(materialized.Mode),
			materialized.Enabled,
			materialized.Bootstrap,
			materialized.ClaimsEnabled,
			materialized.DisplayName,
			materialized.Configuration,
			materialized.LastObservedAt,
		); err != nil {
			return fmt.Errorf("reconcile collector instances: upsert %s: %w", materialized.InstanceID, err)
		}
		ids = append(ids, materialized.InstanceID)
	}
	if len(ids) == 0 {
		ids = []string{""}
	}
	if _, err := execTarget.ExecContext(ctx, deactivateStaleCollectorInstancesQuery, now, ids); err != nil {
		return fmt.Errorf("reconcile collector instances: deactivate stale rows: %w", err)
	}
	if err := commit(); err != nil {
		return fmt.Errorf("reconcile collector instances: commit transaction: %w", err)
	}
	rollback = func() error { return nil }
	return nil
}

// ListCollectorInstances returns the current durable collector instance state.
func (s *WorkflowControlStore) ListCollectorInstances(ctx context.Context) ([]workflow.CollectorInstance, error) {
	if s.db == nil {
		return nil, fmt.Errorf("workflow control store database is required")
	}
	rows, err := s.db.QueryContext(ctx, listCollectorInstancesQuery)
	if err != nil {
		return nil, fmt.Errorf("list collector instances: %w", err)
	}
	defer func() { _ = rows.Close() }()

	instances := make([]workflow.CollectorInstance, 0)
	for rows.Next() {
		instance, err := scanCollectorInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("list collector instances: %w", err)
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list collector instances: %w", err)
	}
	return instances, nil
}

// UpsertCompletenessStates stores reducer-facing checkpoint state per run.
func (s *WorkflowControlStore) UpsertCompletenessStates(ctx context.Context, states []workflow.CompletenessState) error {
	if s.db == nil {
		return fmt.Errorf("workflow control store database is required")
	}
	return s.upsertCompletenessStatesWithExecutor(ctx, s.db, states)
}

func (s *WorkflowControlStore) upsertCompletenessStatesWithExecutor(
	ctx context.Context,
	execTarget Executor,
	states []workflow.CompletenessState,
) error {
	if len(states) == 0 {
		return nil
	}
	for _, state := range states {
		if err := state.Validate(); err != nil {
			return fmt.Errorf("upsert completeness states: %w", err)
		}
		if _, err := execTarget.ExecContext(
			ctx,
			upsertWorkflowRunCompletenessQuery,
			state.RunID,
			string(state.CollectorKind),
			string(state.Keyspace),
			state.PhaseName,
			state.Required,
			strings.TrimSpace(state.Status),
			strings.TrimSpace(state.Detail),
			state.ObservedAt.UTC(),
			state.UpdatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("upsert completeness states: %w", err)
		}
	}
	return nil
}

func scanCollectorInstance(rows Rows) (workflow.CollectorInstance, error) {
	var instance workflow.CollectorInstance
	var collectorKind string
	var mode string
	var deactivatedAt sql.NullTime
	if err := rows.Scan(
		&instance.InstanceID,
		&collectorKind,
		&mode,
		&instance.Enabled,
		&instance.Bootstrap,
		&instance.ClaimsEnabled,
		&instance.DisplayName,
		&instance.Configuration,
		&instance.LastObservedAt,
		&deactivatedAt,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	); err != nil {
		return workflow.CollectorInstance{}, err
	}
	instance.CollectorKind = scope.CollectorKind(strings.TrimSpace(collectorKind))
	instance.Mode = workflow.CollectorMode(strings.TrimSpace(mode))
	if deactivatedAt.Valid {
		instance.DeactivatedAt = deactivatedAt.Time
	}
	return instance, nil
}

func workflowCoordinatorStateBootstrapDefinition() Definition {
	return Definition{
		Name: "workflow_coordinator_state",
		Path: "schema/data-plane/postgres/015_workflow_coordinator_state.sql",
		SQL:  workflowCoordinatorStateSchemaSQL,
	}
}

func init() {
	if !slices.ContainsFunc(bootstrapDefinitions, func(def Definition) bool {
		return def.Name == "workflow_coordinator_state"
	}) {
		bootstrapDefinitions = append(bootstrapDefinitions, workflowCoordinatorStateBootstrapDefinition())
	}
}
