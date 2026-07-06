// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func (s *postgresAdminStore) ListDeadLetterWorkItems(
	ctx context.Context,
	f DeadLetterListFilter,
) ([]AdminDeadLetterWorkItem, error) {
	query, args := buildListDeadLetterWorkItemsQuery(f)
	return scanAdminDeadLetterWorkItems(ctx, s.db, query, args...)
}

func buildListDeadLetterWorkItemsQuery(f DeadLetterListFilter) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
SELECT
    work.work_item_id,
    work.scope_id,
    work.generation_id,
    work.stage,
    work.domain,
    scope.collector_kind,
    work.attempt_count,
    work.failure_class,
    work.created_at,
    work.updated_at,
    work.visible_at
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope ON scope.scope_id = work.scope_id
WHERE work.status = 'dead_letter'
`)
	args := make([]any, 0, 9)
	if value := strings.TrimSpace(f.FailureClass); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND work.failure_class = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.Domain); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND work.domain = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.ScopeID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND work.scope_id = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.CollectorKind); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND scope.collector_kind = $%d\n", len(args))
	}
	if f.UpdatedAfter != nil {
		args = append(args, *f.UpdatedAfter)
		_, _ = fmt.Fprintf(&builder, " AND work.updated_at >= $%d\n", len(args))
	}
	if f.UpdatedBefore != nil {
		args = append(args, *f.UpdatedBefore)
		_, _ = fmt.Fprintf(&builder, " AND work.updated_at < $%d\n", len(args))
	}
	if len(f.AllowedRepositoryIDs) > 0 || len(f.AllowedScopeIDs) > 0 {
		args = append(args, pq.Array(f.AllowedRepositoryIDs))
		repoArg := len(args)
		args = append(args, pq.Array(f.AllowedScopeIDs))
		scopeArg := len(args)
		_, _ = fmt.Fprintf(&builder,
			" AND ((scope.scope_kind = 'repository' AND scope.source_key = ANY($%d)) OR work.scope_id = ANY($%d))\n",
			repoArg,
			scopeArg,
		)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)
	_, _ = fmt.Fprintf(&builder, " ORDER BY work.updated_at DESC, work.work_item_id ASC LIMIT $%d", len(args))
	return builder.String(), args
}

func scanAdminDeadLetterWorkItems(
	ctx context.Context,
	db pgstatus.ExecQueryer,
	query string,
	args ...any,
) ([]AdminDeadLetterWorkItem, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query dead-letter work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []AdminDeadLetterWorkItem
	for rows.Next() {
		var item AdminDeadLetterWorkItem
		var failureClass sql.NullString
		var visibleAt sql.NullTime
		if err := rows.Scan(
			&item.WorkItemID,
			&item.ScopeID,
			&item.GenerationID,
			&item.Stage,
			&item.Domain,
			&item.CollectorKind,
			&item.AttemptCount,
			&failureClass,
			&item.CreatedAt,
			&item.UpdatedAt,
			&visibleAt,
		); err != nil {
			return nil, fmt.Errorf("scan dead-letter work item: %w", err)
		}
		if failureClass.Valid {
			item.FailureClass = &failureClass.String
		}
		if visibleAt.Valid {
			item.VisibleAt = &visibleAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
