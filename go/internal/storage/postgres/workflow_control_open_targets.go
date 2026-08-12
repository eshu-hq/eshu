// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const workflowTerminalRunQuery = `
SELECT EXISTS (
    SELECT 1
    FROM workflow_runs
    WHERE run_id = $1
      AND status IN ('complete', 'failed')
)
`

const workflowAdvisoryTargetLockQuery = `SELECT pg_advisory_xact_lock($1)`

const workflowGuardedRunCreateMaxAttempts = 3

// CreateRunWithWorkItemsIfNoOpenTargets creates a scheduled run only for work
// whose collector target is not already represented by a non-terminal run or
// the same deterministic schedule run.
//
// It returns the number of work-item rows the database actually accepted, which
// can be lower than the number of eligible targets when a unique index drops one
// at insert time. Callers may treat a returned count below len(items) as work
// that was skipped, and it is safe to run more than one coordinator against this
// method: see the read-then-write race described at the advisory lock below.
func (s *WorkflowControlStore) CreateRunWithWorkItemsIfNoOpenTargets(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("workflow control store database is required")
	}
	if s.beginner == nil {
		return 0, fmt.Errorf("workflow control store transaction support is required")
	}
	if err := run.Validate(); err != nil {
		return 0, fmt.Errorf("create guarded workflow run: %w", err)
	}
	for _, item := range items {
		if err := item.Validate(); err != nil {
			return 0, fmt.Errorf("create guarded workflow run: %w", err)
		}
	}
	if len(items) == 0 {
		return 0, nil
	}

	var lastErr error
	for attempt := 1; attempt <= workflowGuardedRunCreateMaxAttempts; attempt++ {
		inserted, err := s.createRunWithWorkItemsIfNoOpenTargetsOnce(ctx, run, items)
		if err == nil {
			return inserted, nil
		}
		lastErr = err
		if !isRetryableWorkflowReconciliationError(err) || attempt >= workflowGuardedRunCreateMaxAttempts {
			break
		}
	}
	return 0, lastErr
}

func (s *WorkflowControlStore) createRunWithWorkItemsIfNoOpenTargetsOnce(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (int, error) {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("create guarded workflow run: begin transaction: %w", err)
	}
	rollback := tx.Rollback
	defer func() { _ = rollback() }()

	// This lock is what makes running more than one coordinator safe (#4586).
	// It must stay here, ahead of every read below.
	//
	// Two coordinators ticking either side of a 30-second wall-clock bucket edge
	// plan the SAME collector target under different run, work-item, and
	// generation identifiers. Those rows are legitimately distinct, so neither
	// the primary key nor ON CONFLICT DO NOTHING collapses them, and the
	// NOT EXISTS in workItemsWithoutOpenTargets cannot help on its own: under
	// Read Committed both transactions take their snapshot before either
	// commits, so both read "nothing planned" and both insert. The target ends
	// up with two independently claimable pending work items and gets collected
	// twice.
	//
	// pg_advisory_xact_lock keyed on (collector_kind, collector_instance_id)
	// closes that window by making the read and the insert one atomic step per
	// collector instance. The key deliberately carries no bucket, run, or
	// generation, so competing coordinators contend on the same key instead of
	// passing each other. It is transaction-scoped, so it releases on commit or
	// rollback with no unlock path to leak.
	//
	// Measured on Postgres 16 at Read Committed, running this guard body twice
	// differing in exactly this one statement: without the lock, 25 of 25 runs
	// produced a duplicate pending work item; with it, 0 of 25.
	//
	// Guarded by TestWorkflowGuardedRunCreateAdmitsOneTargetForConcurrentCoordinatorsLive
	// and TestWorkflowGuardedRunCreateWaitsOnPlanningLockBeforeReadingTargetsLive
	// (real Postgres), plus the hermetic
	// TestWorkflowControlStoreGuardedRunTakesPlanningLockBeforeReadingTargets,
	// which fails if the lock is removed OR merely moved after the read.
	if err := lockWorkflowOpenTargets(ctx, tx, items); err != nil {
		return 0, err
	}
	terminal, err := s.workflowRunIsTerminal(ctx, tx, run.RunID)
	if err != nil {
		return 0, err
	}
	if terminal {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("create guarded workflow run: commit terminal-run skip: %w", err)
		}
		rollback = func() error { return nil }
		return 0, nil
	}
	eligible, err := s.workItemsWithoutOpenTargets(ctx, tx, run.RunID, items)
	if err != nil {
		return 0, err
	}
	if len(eligible) == 0 {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("create guarded workflow run: commit skipped transaction: %w", err)
		}
		rollback = func() error { return nil }
		return 0, nil
	}
	if err := s.createRunWithExecutor(ctx, tx, run); err != nil {
		return 0, err
	}
	// Count rows the database accepted, not targets we planned. The INSERT ends
	// in ON CONFLICT DO NOTHING and workflow_work_items carries partial unique
	// indexes, so a planned target can be dropped at insert time. Reporting
	// len(eligible) here told the coordinator every target was enqueued, which
	// suppressed its "skipped duplicate workflow work" log and left an operator
	// reading an enqueued count for work that does not exist (#4586).
	var inserted int64
	for i := 0; i < len(eligible); i += workflowEnqueueBatchSize {
		end := i + workflowEnqueueBatchSize
		if end > len(eligible) {
			end = len(eligible)
		}
		batchInserted, err := s.enqueueWorkItemBatchWithExecutor(ctx, tx, eligible[i:end])
		if err != nil {
			return 0, err
		}
		inserted += batchInserted
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("create guarded workflow run: commit transaction: %w", err)
	}
	rollback = func() error { return nil }
	return int(inserted), nil
}

func lockWorkflowOpenTargets(ctx context.Context, executor Executor, items []workflow.WorkItem) error {
	keys := workflowPlanningLockKeys(items)
	for _, key := range keys {
		if _, err := executor.ExecContext(ctx, workflowAdvisoryTargetLockQuery, key); err != nil {
			return fmt.Errorf("create guarded workflow run: lock target: %w", err)
		}
	}
	return nil
}

func workflowPlanningLockKeys(items []workflow.WorkItem) []int64 {
	unique := make(map[int64]struct{}, len(items))
	for _, item := range items {
		unique[workflowPlanningLockKey(item)] = struct{}{}
	}
	keys := make([]int64, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func workflowPlanningLockKey(item workflow.WorkItem) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(workflowPlanningLockKeyValue(item)))
	return int64(hasher.Sum64()) // #nosec G115 -- bounded: intentional bit-reinterpret of FNV-64 hash to int64 for pg_advisory_lock; full 64-bit range is the design
}

func workflowPlanningLockKeyValue(item workflow.WorkItem) string {
	return strings.Join([]string{
		string(item.CollectorKind),
		strings.TrimSpace(item.CollectorInstanceID),
	}, "\x00")
}

func (s *WorkflowControlStore) workItemsWithoutOpenTargets(
	ctx context.Context,
	queryer Queryer,
	runID string,
	items []workflow.WorkItem,
) ([]workflow.WorkItem, error) {
	if len(items) == 0 {
		return nil, nil
	}
	query, args := workflowEligibleTargetsQuery(runID, items)
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("create guarded workflow run: read eligible targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	eligible := make([]workflow.WorkItem, 0, len(items))
	for rows.Next() {
		var ordinal int
		if err := rows.Scan(&ordinal); err != nil {
			return nil, fmt.Errorf("create guarded workflow run: read eligible targets: %w", err)
		}
		if ordinal < 0 || ordinal >= len(items) {
			return nil, fmt.Errorf("create guarded workflow run: eligible target ordinal %d out of range", ordinal)
		}
		eligible = append(eligible, items[ordinal])
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("create guarded workflow run: read eligible targets: %w", err)
	}
	return eligible, nil
}

func workflowEligibleTargetsQuery(runID string, items []workflow.WorkItem) (string, []any) {
	args := make([]any, 0, 1+len(items)*9)
	args = append(args, runID)

	var values strings.Builder
	for i, item := range items {
		if i > 0 {
			values.WriteString(",\n")
		}
		base := 2 + i*9
		fmt.Fprintf(
			&values,
			"    ($%d::int, $%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::text)",
			base,
			base+1,
			base+2,
			base+3,
			base+4,
			base+5,
			base+6,
			base+7,
			base+8,
		)
		args = append(
			args,
			i,
			string(item.CollectorKind),
			item.CollectorInstanceID,
			item.ScopeID,
			item.TenantID,
			item.WorkspaceID,
			item.SubjectClass,
			item.PolicyRevisionHash,
			item.AcceptanceUnitID,
		)
	}

	query := fmt.Sprintf(`
WITH planned_targets(
    ordinal,
    collector_kind,
    collector_instance_id,
    scope_id,
    tenant_id,
    workspace_id,
    subject_class,
    policy_revision_hash,
    acceptance_unit_id
) AS (
VALUES
%s
)
SELECT planned.ordinal
FROM planned_targets AS planned
WHERE NOT EXISTS (
    SELECT 1
    FROM workflow_work_items AS item
    JOIN workflow_runs AS run
      ON run.run_id = item.run_id
    WHERE item.collector_kind = planned.collector_kind
      AND item.collector_instance_id = planned.collector_instance_id
      AND item.scope_id = planned.scope_id
      AND item.tenant_id = planned.tenant_id
      AND item.workspace_id = planned.workspace_id
      AND item.subject_class = planned.subject_class
      AND item.policy_revision_hash = planned.policy_revision_hash
      AND item.acceptance_unit_id = planned.acceptance_unit_id
      AND run.status NOT IN ('complete', 'failed')
)
AND NOT EXISTS (
    SELECT 1
    FROM workflow_work_items AS item
    WHERE item.run_id = $1
      AND item.collector_kind = planned.collector_kind
      AND item.collector_instance_id = planned.collector_instance_id
      AND item.scope_id = planned.scope_id
      AND item.tenant_id = planned.tenant_id
      AND item.workspace_id = planned.workspace_id
      AND item.subject_class = planned.subject_class
      AND item.policy_revision_hash = planned.policy_revision_hash
      AND item.acceptance_unit_id = planned.acceptance_unit_id
)
ORDER BY planned.ordinal
`, values.String())
	return query, args
}

func (s *WorkflowControlStore) workflowRunIsTerminal(
	ctx context.Context,
	queryer Queryer,
	runID string,
) (bool, error) {
	rows, err := queryer.QueryContext(ctx, workflowTerminalRunQuery, runID)
	if err != nil {
		return false, fmt.Errorf("create guarded workflow run: read terminal run: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("create guarded workflow run: read terminal run: %w", err)
		}
		return false, fmt.Errorf("create guarded workflow run: terminal run query returned no rows")
	}
	var terminal bool
	if err := rows.Scan(&terminal); err != nil {
		return false, fmt.Errorf("create guarded workflow run: read terminal run: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("create guarded workflow run: read terminal run: %w", err)
	}
	return terminal, nil
}
