// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// enqueueWorkItemBatchScopesWithExecutor inserts one batch and reports the
// scope ids the database actually accepted. The INSERT ends in ON CONFLICT DO
// NOTHING RETURNING scope_id, so only rows the table accepted come back: a
// planned target a partial unique index dropped never appears. Callers that
// join a commit to the inserted set MUST use this list rather than the
// eligible slice (eshu-hq/eshu#10149).
//
// One INSERT ... ON CONFLICT DO NOTHING can never return more rows than it
// was given, so the returned count carries the same 0..len(items) bound the
// count path enforces (#4586).
func (s *WorkflowControlStore) enqueueWorkItemBatchScopesWithExecutor(
	ctx context.Context,
	executor db.ExecQueryer,
	items []workflow.WorkItem,
) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	query, args := enqueueWorkflowWorkItemsQuery(items, enqueueWorkflowWorkItemsScopesSuffix)
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("enqueue workflow work item batch (%d items): %w", len(items), err)
	}
	defer func() { _ = rows.Close() }()
	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("enqueue workflow work item batch (%d items): read inserted scope: %w", len(items), err)
		}
		scopes = append(scopes, scopeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("enqueue workflow work item batch (%d items): read inserted scopes: %w", len(items), err)
	}
	if len(scopes) > len(items) {
		return nil, fmt.Errorf(
			"enqueue workflow work item batch (%d items): returned scopes %d is outside 0..%d",
			len(items),
			len(scopes),
			len(items),
		)
	}
	return scopes, nil
}
