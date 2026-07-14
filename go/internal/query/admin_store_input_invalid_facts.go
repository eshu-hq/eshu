// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// ListReducerInputInvalidFacts implements AdminStore for the durable
// reducer_input_invalid_facts read surface (issue #4630).
func (s *postgresAdminStore) ListReducerInputInvalidFacts(
	ctx context.Context,
	f InputInvalidFactListFilter,
) ([]AdminReducerInputInvalidFact, error) {
	query, args := buildListReducerInputInvalidFactsQuery(f)
	return scanAdminReducerInputInvalidFacts(ctx, s.db, query, args...)
}

func buildListReducerInputInvalidFactsQuery(f InputInvalidFactListFilter) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
SELECT
    quarantine.fact_id,
    quarantine.fact_kind,
    quarantine.missing_field,
    quarantine.failure_class,
    quarantine.domain,
    quarantine.scope_id,
    quarantine.generation_id,
    quarantine.decided_at
FROM reducer_input_invalid_facts AS quarantine
JOIN ingestion_scopes AS scope ON scope.scope_id = quarantine.scope_id
WHERE quarantine.scope_id = $1
  AND quarantine.generation_id = $2
`)
	args := []any{f.ScopeID, f.GenerationID}
	if value := strings.TrimSpace(f.Domain); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND quarantine.domain = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.FactKind); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND quarantine.fact_kind = $%d\n", len(args))
	}
	// Authorize the requested scope via ingestion_scopes, mirroring
	// buildListDeadLetterWorkItemsQuery: a scoped token that grants a
	// repository (not the raw scope_id) is authorized when the requested
	// scope's source_key matches an allowed repository, OR when the raw
	// scope_id itself was granted directly. Without this join, a
	// repository-scoped token that never received the raw ingestion
	// scope_id could never read its own quarantine rows (codex review on
	// PR #5252, issue #4630).
	if len(f.AllowedRepositoryIDs) > 0 || len(f.AllowedScopeIDs) > 0 {
		args = append(args, pq.Array(f.AllowedRepositoryIDs))
		repoArg := len(args)
		args = append(args, pq.Array(f.AllowedScopeIDs))
		scopeArg := len(args)
		_, _ = fmt.Fprintf(&builder,
			" AND ((scope.scope_kind = 'repository' AND scope.source_key = ANY($%d)) OR quarantine.scope_id = ANY($%d))\n",
			repoArg,
			scopeArg,
		)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)
	_, _ = fmt.Fprintf(&builder,
		" ORDER BY quarantine.decided_at DESC, quarantine.fact_id ASC, quarantine.missing_field ASC LIMIT $%d",
		len(args),
	)
	return builder.String(), args
}

func scanAdminReducerInputInvalidFacts(
	ctx context.Context,
	db pgstatus.ExecQueryer,
	query string,
	args ...any,
) ([]AdminReducerInputInvalidFact, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query reducer input_invalid facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []AdminReducerInputInvalidFact
	for rows.Next() {
		var item AdminReducerInputInvalidFact
		if err := rows.Scan(
			&item.FactID,
			&item.FactKind,
			&item.MissingField,
			&item.FailureClass,
			&item.Domain,
			&item.ScopeID,
			&item.GenerationID,
			&item.DecidedAt,
		); err != nil {
			return nil, fmt.Errorf("scan reducer input_invalid fact: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
