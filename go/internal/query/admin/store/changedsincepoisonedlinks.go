// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// ListChangedSincePoisonedLinks reads a bounded, deterministic page of
// changed_since_scope_cursor rows that are poisoned (poisoned_activation_seq
// set) or retrying (a counted failure pending, attempt_count > 0), the
// changed-since link writer's own ledger (#7290,
// go/internal/storage/postgres/freshness/links, #7127 ruling 8.10). Neither
// state is a fact_work_items dead letter, so this is the only read surface
// for them.
func (s *postgresReadStore) ListChangedSincePoisonedLinks(
	ctx context.Context,
	f admin.ChangedSincePoisonedLinkFilter,
) ([]admin.ChangedSincePoisonedLink, error) {
	query, args := buildListChangedSincePoisonedLinksQuery(f)
	return scanChangedSincePoisonedLinks(ctx, s.database, query, args...)
}

func buildListChangedSincePoisonedLinksQuery(f admin.ChangedSincePoisonedLinkFilter) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
SELECT
    cursor.scope_id,
    cursor.poisoned_activation_seq,
    cursor.attempt_activation_seq,
    cursor.attempt_count,
    cursor.next_attempt_at,
    cursor.last_failure_class,
    cursor.poisoned_at,
    cursor.updated_at
FROM changed_since_scope_cursor AS cursor
JOIN ingestion_scopes AS scope ON scope.scope_id = cursor.scope_id
WHERE (cursor.poisoned_activation_seq IS NOT NULL OR cursor.attempt_count > 0)
`)
	args := make([]any, 0, 8)
	switch strings.TrimSpace(f.Status) {
	case "poisoned":
		builder.WriteString(" AND cursor.poisoned_activation_seq IS NOT NULL\n")
	case "retrying":
		builder.WriteString(" AND cursor.poisoned_activation_seq IS NULL AND cursor.attempt_count > 0\n")
	}
	if value := strings.TrimSpace(f.ScopeID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND cursor.scope_id = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.Cursor); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND cursor.scope_id > $%d\n", len(args))
	}
	if len(f.AllowedRepositoryIDs) > 0 || len(f.AllowedScopeIDs) > 0 {
		args = append(args, array.Of(f.AllowedRepositoryIDs))
		repoArg := len(args)
		args = append(args, array.Of(f.AllowedScopeIDs))
		scopeArg := len(args)
		_, _ = fmt.Fprintf(&builder,
			" AND ((scope.scope_kind = 'repository' AND scope.source_key = ANY($%d)) OR cursor.scope_id = ANY($%d))\n",
			repoArg,
			scopeArg,
		)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)
	_, _ = fmt.Fprintf(&builder, " ORDER BY cursor.scope_id ASC LIMIT $%d", len(args))
	return builder.String(), args
}

func scanChangedSincePoisonedLinks(
	ctx context.Context,
	database db.Queryer,
	query string,
	args ...any,
) ([]admin.ChangedSincePoisonedLink, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query changed-since poisoned links: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []admin.ChangedSincePoisonedLink
	for rows.Next() {
		var item admin.ChangedSincePoisonedLink
		var poisonedActivationSeq sql.NullInt64
		var attemptActivationSeq sql.NullInt64
		var nextAttemptAt sql.NullTime
		var lastFailureClass sql.NullString
		var poisonedAt sql.NullTime
		if err := rows.Scan(
			&item.ScopeID,
			&poisonedActivationSeq,
			&attemptActivationSeq,
			&item.AttemptCount,
			&nextAttemptAt,
			&lastFailureClass,
			&poisonedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan changed-since poisoned link: %w", err)
		}
		if poisonedActivationSeq.Valid {
			item.Status = "poisoned"
			item.ActivationSeq = poisonedActivationSeq.Int64
		} else {
			item.Status = "retrying"
			item.ActivationSeq = attemptActivationSeq.Int64
		}
		if lastFailureClass.Valid {
			item.LastFailureClass = &lastFailureClass.String
		}
		if nextAttemptAt.Valid {
			item.NextAttemptAt = &nextAttemptAt.Time
		}
		if poisonedAt.Valid {
			item.PoisonedAt = &poisonedAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
