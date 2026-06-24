// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const isCurrentGenerationSQL = `
SELECT active_generation_id
FROM ingestion_scopes
WHERE scope_id = $1
`

const priorGenerationExistsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM scope_generations
    WHERE scope_id = $1
      AND generation_id <> $2
)
`

// CurrentScopeGeneration reports the newest pending or active generation with
// a non-empty freshness hint for one scope.
type CurrentScopeGeneration struct {
	GenerationID  string
	FreshnessHint string
}

// CurrentScopeGeneration returns the newest pending or active generation with
// a non-empty freshness hint for one scope.
func (s IngestionStore) CurrentScopeGeneration(
	ctx context.Context,
	scopeID string,
) (CurrentScopeGeneration, bool, error) {
	if s.db == nil {
		return CurrentScopeGeneration{}, false, fmt.Errorf("ingestion store db is required")
	}
	rows, err := s.db.QueryContext(ctx, activeGenerationFreshnessQuery, scopeID)
	if err != nil {
		return CurrentScopeGeneration{}, false, fmt.Errorf("query current generation for scope %s: %w", scopeID, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return CurrentScopeGeneration{}, false, fmt.Errorf("query current generation for scope %s: %w", scopeID, err)
		}
		return CurrentScopeGeneration{}, false, nil
	}

	var current CurrentScopeGeneration
	if err := rows.Scan(&current.GenerationID, &current.FreshnessHint); err != nil {
		return CurrentScopeGeneration{}, false, fmt.Errorf("scan current generation for scope %s: %w", scopeID, err)
	}
	current.GenerationID = strings.TrimSpace(current.GenerationID)
	current.FreshnessHint = strings.TrimSpace(current.FreshnessHint)
	return current, current.GenerationID != "", rows.Err()
}

// NewGenerationFreshnessCheck returns a GenerationFreshnessCheck backed by
// the ingestion_scopes.active_generation_id denormalized column.
func NewGenerationFreshnessCheck(db ExecQueryer) reducer.GenerationFreshnessCheck {
	return func(ctx context.Context, scopeID, generationID string) (bool, error) {
		rows, err := db.QueryContext(ctx, isCurrentGenerationSQL, scopeID)
		if err != nil {
			return false, fmt.Errorf("query active generation for scope %s: %w", scopeID, err)
		}
		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			// Unknown scope — assume current (defensive, let handler decide).
			return true, nil
		}

		var activeGenID sql.NullString
		if err := rows.Scan(&activeGenID); err != nil {
			return false, fmt.Errorf("scan active generation for scope %s: %w", scopeID, err)
		}

		if !activeGenID.Valid {
			// No active generation yet — assume current.
			return true, nil
		}

		return activeGenID.String == generationID, nil
	}
}

// NewPriorGenerationCheck returns a check backed by scope_generations for
// identifying first-generation writes.
func NewPriorGenerationCheck(db ExecQueryer) reducer.PriorGenerationCheck {
	return func(ctx context.Context, scopeID, generationID string) (bool, error) {
		rows, err := db.QueryContext(ctx, priorGenerationExistsSQL, scopeID, generationID)
		if err != nil {
			return false, fmt.Errorf("query prior generation for scope %s: %w", scopeID, err)
		}
		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			return false, nil
		}

		var exists bool
		if err := rows.Scan(&exists); err != nil {
			return false, fmt.Errorf("scan prior generation for scope %s: %w", scopeID, err)
		}
		return exists, nil
	}
}
