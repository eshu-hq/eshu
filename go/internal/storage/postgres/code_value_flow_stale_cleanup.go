// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const listCurrentCodeValueFlowGenerationsQuery = `
SELECT
    scope.scope_id,
    scope.active_generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
WHERE scope.active_generation_id IS NOT NULL
  AND scope.scope_kind = 'repository'
  AND ($1 = '' OR scope.scope_id > $1)
ORDER BY scope.scope_id ASC
LIMIT $2
`

// CodeValueFlowCurrentGenerationStore lists active repository-scope generations
// that can own reducer value-flow evidence.
type CodeValueFlowCurrentGenerationStore struct {
	db ExecQueryer
}

// NewCodeValueFlowCurrentGenerationStore returns a bounded active-generation
// reader for reducer value-flow stale cleanup.
func NewCodeValueFlowCurrentGenerationStore(db ExecQueryer) CodeValueFlowCurrentGenerationStore {
	return CodeValueFlowCurrentGenerationStore{db: db}
}

// ListCurrentCodeValueFlowGenerations returns a deterministic page of active
// repository scopes after the supplied scope cursor.
func (s CodeValueFlowCurrentGenerationStore) ListCurrentCodeValueFlowGenerations(
	ctx context.Context,
	afterScopeID string,
	limit int,
) ([]reducer.CodeValueFlowCurrentGeneration, error) {
	if limit <= 0 {
		return nil, nil
	}
	if s.db == nil {
		return nil, fmt.Errorf("code value-flow current generation store database is required")
	}
	rows, err := s.db.QueryContext(ctx, listCurrentCodeValueFlowGenerationsQuery, strings.TrimSpace(afterScopeID), limit)
	if err != nil {
		return nil, fmt.Errorf("query current code value-flow generations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	generations := make([]reducer.CodeValueFlowCurrentGeneration, 0, limit)
	for rows.Next() {
		var row reducer.CodeValueFlowCurrentGeneration
		if err := rows.Scan(&row.ScopeID, &row.GenerationID); err != nil {
			return nil, fmt.Errorf("scan current code value-flow generation: %w", err)
		}
		row.ScopeID = strings.TrimSpace(row.ScopeID)
		row.GenerationID = strings.TrimSpace(row.GenerationID)
		if row.ScopeID == "" || row.GenerationID == "" {
			continue
		}
		generations = append(generations, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current code value-flow generations: %w", err)
	}
	return generations, nil
}
