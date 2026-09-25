// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// generationFreshnessSQL reads, in one statement (one snapshot), the scope's
// active generation plus the intent generation's status and whether that
// generation sorts after the active one. The order is (ingested_at,
// generation_id): the same order the projector supersession SQL
// (supersedeProjectorObsoleteGenerationsQuery and the running-work heartbeat
// check) and the reducer claim's superseded_stale_reducer_generations CTE use
// to decide which generation wins, so "newer" here means "the projector will
// let this generation activate over the current one". Every join is a primary
// key lookup (ingestion_scopes.scope_id, scope_generations.generation_id). An
// unknown scope returns no rows.
const generationFreshnessSQL = `
SELECT scope.active_generation_id,
       intent_generation.status,
       COALESCE(
           (intent_generation.ingested_at, intent_generation.generation_id)
               > (active_generation.ingested_at, active_generation.generation_id),
           false
       ) AS intent_is_newer
FROM ingestion_scopes AS scope
LEFT JOIN scope_generations AS intent_generation
       ON intent_generation.generation_id = $2
      AND intent_generation.scope_id = scope.scope_id
LEFT JOIN scope_generations AS active_generation
       ON active_generation.generation_id = scope.active_generation_id
      AND active_generation.scope_id = scope.scope_id
WHERE scope.scope_id = $1
`

const priorGenerationExistsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM scope_generations
    WHERE scope_id = $1
      AND generation_id <> $2
)
`

// priorGenerationIDSQL resolves the strictly-older generation: the newest
// generation whose (observed_at, generation_id) sorts before the intent's
// own row. A newest-other lookup would return a NEWER generation whenever a
// non-newest intent executes (retry crossing a scan boundary, backfill,
// queue lag), and the diff would then yield newly-created uids as retract
// candidates. A missing intent row (or no older generation) yields no rows,
// which NewPriorGenerationID reports as found=false: the handler skips the
// retract, which is the safe direction.
const priorGenerationIDSQL = `
SELECT generation_id
FROM scope_generations
WHERE scope_id = $1
  AND (observed_at, generation_id) < (
    SELECT observed_at, generation_id
    FROM scope_generations
    WHERE scope_id = $1 AND generation_id = $2
  )
ORDER BY observed_at DESC, generation_id DESC
LIMIT 1
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
	if s.database == nil {
		return CurrentScopeGeneration{}, false, fmt.Errorf("ingestion store db is required")
	}
	rows, err := s.database.QueryContext(ctx, activeGenerationFreshnessQuery, scopeID)
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
// the ingestion_scopes.active_generation_id denormalized column and the
// scope_generations lifecycle row of the intent's generation.
//
// Outcomes:
//   - the intent generation is active, the scope has no active generation, or
//     the scope is unknown: (true, nil), and the handler runs;
//   - the intent generation is still pending and sorts after the active
//     generation: (false, reducercontract.GenerationNotYetActiveError). Its
//     projector has enqueued reducer work but not yet acknowledged, so the
//     intent is retried (non-counting) until the generation activates, rather
//     than acked succeeded as superseded and never re-driven (#6686);
//   - anything else (an older, superseded, or failed generation, or a missing
//     generation row): (false, nil), and the intent is terminally superseded.
func NewGenerationFreshnessCheck(database db.ExecQueryer) reducer.GenerationFreshnessCheck {
	return func(ctx context.Context, scopeID, generationID string) (bool, error) {
		rows, err := database.QueryContext(ctx, generationFreshnessSQL, scopeID, generationID)
		if err != nil {
			return false, fmt.Errorf("query active generation for scope %s: %w", scopeID, err)
		}
		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return false, fmt.Errorf("query active generation for scope %s: %w", scopeID, err)
			}
			// Unknown scope — assume current (defensive, let handler decide).
			return true, nil
		}

		var (
			activeGenID   sql.NullString
			intentStatus  sql.NullString
			intentIsNewer bool
		)
		if err := rows.Scan(&activeGenID, &intentStatus, &intentIsNewer); err != nil {
			return false, fmt.Errorf("scan active generation for scope %s: %w", scopeID, err)
		}

		if !activeGenID.Valid {
			// No active generation yet — assume current.
			return true, nil
		}
		if activeGenID.String == generationID {
			return true, nil
		}
		if intentStatus.Valid && intentStatus.String == string(scope.GenerationStatusPending) && intentIsNewer {
			return false, reducercontract.GenerationNotYetActiveError{
				ScopeID:            scopeID,
				GenerationID:       generationID,
				ActiveGenerationID: activeGenID.String,
			}
		}
		return false, nil
	}
}

// NewPriorGenerationID returns the predecessor-generation lookup backed by
// scope_generations. The ordering is observation time with the generation id
// as the deterministic tie-break, so concurrent same-timestamp generations
// still resolve one predecessor. A scope with no earlier generation reports
// found=false (first write: no diff source, no retract).
func NewPriorGenerationID(database db.ExecQueryer) reducer.PriorGenerationID {
	return func(ctx context.Context, scopeID, generationID string) (string, bool, error) {
		rows, err := database.QueryContext(ctx, priorGenerationIDSQL, scopeID, generationID)
		if err != nil {
			return "", false, fmt.Errorf("query prior generation id for scope %s: %w", scopeID, err)
		}
		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return "", false, fmt.Errorf("iterate prior generation id for scope %s: %w", scopeID, err)
			}
			return "", false, nil
		}

		var prior string
		if err := rows.Scan(&prior); err != nil {
			return "", false, fmt.Errorf("scan prior generation id for scope %s: %w", scopeID, err)
		}
		if err := rows.Err(); err != nil {
			return "", false, fmt.Errorf("iterate prior generation id for scope %s: %w", scopeID, err)
		}
		return strings.TrimSpace(prior), strings.TrimSpace(prior) != "", nil
	}
}

// NewPriorGenerationCheck returns a check backed by scope_generations for
// identifying first-generation writes.
func NewPriorGenerationCheck(database db.ExecQueryer) reducer.PriorGenerationCheck {
	return func(ctx context.Context, scopeID, generationID string) (bool, error) {
		rows, err := database.QueryContext(ctx, priorGenerationExistsSQL, scopeID, generationID)
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
