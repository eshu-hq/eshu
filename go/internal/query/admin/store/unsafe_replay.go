// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// UnsafeReplayTargets returns the replay-eligible (dead_letter or failed) work
// items among f.WorkItemIDs whose failure_class is in f.UnsafeFailureClasses.
// It is one primary-key-bounded read, so it returns at most len(WorkItemIDs)
// rows and never scans the queue. Ids that do not exist, are not terminal, or
// have a safe or NULL failure_class produce no row, matching the eligibility
// the replay UPDATE itself applies.
func (s *postgresStore) UnsafeReplayTargets(
	ctx context.Context,
	f admin.UnsafeReplayTargetFilter,
) ([]admin.UnsafeReplayTarget, error) {
	if len(f.WorkItemIDs) == 0 || len(f.UnsafeFailureClasses) == 0 {
		return nil, nil
	}
	query, args := buildUnsafeReplayTargetsQuery(f)
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query unsafe replay targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var targets []admin.UnsafeReplayTarget
	for rows.Next() {
		var target admin.UnsafeReplayTarget
		if err := rows.Scan(&target.WorkItemID, &target.FailureClass); err != nil {
			return nil, fmt.Errorf("scan unsafe replay target: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unsafe replay targets: %w", err)
	}
	return targets, nil
}

// buildUnsafeReplayTargetsQuery renders the unsafe-target read. The optional
// scope and stage predicates mirror buildMutatingWorkItemsQuery so the read
// and the replay agree on the candidate set.
func buildUnsafeReplayTargetsQuery(f admin.UnsafeReplayTargetFilter) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
SELECT work_item_id, failure_class
FROM fact_work_items
WHERE status IN ('dead_letter', 'failed')
  AND work_item_id = ANY($1)
  AND failure_class = ANY($2)
`)
	args := []any{f.WorkItemIDs, f.UnsafeFailureClasses}
	if value := strings.TrimSpace(f.ScopeID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, "  AND scope_id = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.Stage); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, "  AND stage = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.FailureClass); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, "  AND failure_class = $%d\n", len(args))
	}
	builder.WriteString("ORDER BY work_item_id ASC\n")
	return builder.String(), args
}
