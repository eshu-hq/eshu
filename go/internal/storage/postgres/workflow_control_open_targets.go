package postgres

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const workflowOpenTargetQuery = `
SELECT EXISTS (
    SELECT 1
    FROM workflow_work_items AS item
    JOIN workflow_runs AS run
      ON run.run_id = item.run_id
    WHERE item.collector_kind = $1
      AND item.collector_instance_id = $2
      AND item.scope_id = $3
      AND item.acceptance_unit_id = $4
      AND run.status NOT IN ('complete', 'failed')
)
`

const workflowSameRunTargetQuery = `
SELECT EXISTS (
    SELECT 1
    FROM workflow_work_items AS item
    WHERE item.run_id = $1
      AND item.collector_kind = $2
      AND item.collector_instance_id = $3
      AND item.scope_id = $4
      AND item.acceptance_unit_id = $5
)
`

const workflowTerminalRunQuery = `
SELECT EXISTS (
    SELECT 1
    FROM workflow_runs
    WHERE run_id = $1
      AND status IN ('complete', 'failed')
)
`

const workflowAdvisoryTargetLockQuery = `SELECT pg_advisory_xact_lock($1)`

// CreateRunWithWorkItemsIfNoOpenTargets creates a scheduled run only for work
// whose collector target is not already represented by a non-terminal run or
// the same deterministic schedule run.
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

	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("create guarded workflow run: begin transaction: %w", err)
	}
	rollback := tx.Rollback
	defer func() { _ = rollback() }()

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
	for i := 0; i < len(eligible); i += workflowEnqueueBatchSize {
		end := i + workflowEnqueueBatchSize
		if end > len(eligible) {
			end = len(eligible)
		}
		if err := s.enqueueWorkItemBatchWithExecutor(ctx, tx, eligible[i:end]); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("create guarded workflow run: commit transaction: %w", err)
	}
	rollback = func() error { return nil }
	return len(eligible), nil
}

func lockWorkflowOpenTargets(ctx context.Context, executor Executor, items []workflow.WorkItem) error {
	keys := workflowOpenTargetLockKeys(items)
	for _, key := range keys {
		if _, err := executor.ExecContext(ctx, workflowAdvisoryTargetLockQuery, key); err != nil {
			return fmt.Errorf("create guarded workflow run: lock target: %w", err)
		}
	}
	return nil
}

func workflowOpenTargetLockKeys(items []workflow.WorkItem) []int64 {
	unique := make(map[int64]struct{}, len(items))
	for _, item := range items {
		unique[workflowOpenTargetLockKey(item)] = struct{}{}
	}
	keys := make([]int64, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func workflowOpenTargetLockKey(item workflow.WorkItem) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(workflowOpenTargetKey(item)))
	return int64(hasher.Sum64())
}

func workflowOpenTargetKey(item workflow.WorkItem) string {
	return strings.Join([]string{
		string(item.CollectorKind),
		strings.TrimSpace(item.CollectorInstanceID),
		strings.TrimSpace(item.ScopeID),
		strings.TrimSpace(item.AcceptanceUnitID),
	}, "\x00")
}

func (s *WorkflowControlStore) workItemsWithoutOpenTargets(
	ctx context.Context,
	queryer Queryer,
	runID string,
	items []workflow.WorkItem,
) ([]workflow.WorkItem, error) {
	eligible := make([]workflow.WorkItem, 0, len(items))
	for _, item := range items {
		open, err := s.workflowTargetHasOpenRun(ctx, queryer, item)
		if err != nil {
			return nil, err
		}
		if open {
			continue
		}
		sameRun, err := s.workflowTargetExistsForRun(ctx, queryer, runID, item)
		if err != nil {
			return nil, err
		}
		if sameRun {
			continue
		}
		eligible = append(eligible, item)
	}
	return eligible, nil
}

func (s *WorkflowControlStore) workflowTargetHasOpenRun(
	ctx context.Context,
	queryer Queryer,
	item workflow.WorkItem,
) (bool, error) {
	rows, err := queryer.QueryContext(
		ctx,
		workflowOpenTargetQuery,
		string(item.CollectorKind),
		item.CollectorInstanceID,
		item.ScopeID,
		item.AcceptanceUnitID,
	)
	if err != nil {
		return false, fmt.Errorf("create guarded workflow run: read open target: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("create guarded workflow run: read open target: %w", err)
		}
		return false, fmt.Errorf("create guarded workflow run: open target query returned no rows")
	}
	var open bool
	if err := rows.Scan(&open); err != nil {
		return false, fmt.Errorf("create guarded workflow run: read open target: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("create guarded workflow run: read open target: %w", err)
	}
	return open, nil
}

func (s *WorkflowControlStore) workflowTargetExistsForRun(
	ctx context.Context,
	queryer Queryer,
	runID string,
	item workflow.WorkItem,
) (bool, error) {
	rows, err := queryer.QueryContext(
		ctx,
		workflowSameRunTargetQuery,
		runID,
		string(item.CollectorKind),
		item.CollectorInstanceID,
		item.ScopeID,
		item.AcceptanceUnitID,
	)
	if err != nil {
		return false, fmt.Errorf("create guarded workflow run: read same-run target: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("create guarded workflow run: read same-run target: %w", err)
		}
		return false, fmt.Errorf("create guarded workflow run: same-run target query returned no rows")
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("create guarded workflow run: read same-run target: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("create guarded workflow run: read same-run target: %w", err)
	}
	return exists, nil
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
