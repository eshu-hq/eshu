// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
)

// RelationshipGenerationActiveChecker reports whether a relationship generation
// is active (published) in Postgres. RelationshipStore satisfies it.
type RelationshipGenerationActiveChecker interface {
	IsGenerationActive(ctx context.Context, generationID string) (bool, error)
}

// NewRelationshipGenerationActiveLookup adapts a generation active-status
// checker to the reducer authority gate. It backs the repo-dependency lane so an
// accepted generation only grants graph-projection authority once the
// relationship generation is activated, keeping the graph from running ahead of
// the Postgres relationship read models.
func NewRelationshipGenerationActiveLookup(
	checker RelationshipGenerationActiveChecker,
) maintenance.RelationshipGenerationActiveLookup {
	return func(generationID string) (bool, error) {
		generationID = strings.TrimSpace(generationID)
		if generationID == "" {
			return false, nil
		}
		return checker.IsGenerationActive(context.Background(), generationID)
	}
}

// RelationshipGenerationsCompleteChecker reports whether every active scope's
// current relationship generation is active. RelationshipStore satisfies it.
type RelationshipGenerationsCompleteChecker interface {
	AreActiveScopeRelationshipGenerationsComplete(ctx context.Context) (bool, error)
}

// NewRelationshipGenerationsCompleteLookup adapts a corpus-wide completeness
// checker to the workload and deployable-unit correlation input gates. A
// lookup error fails safe as incomplete: derivation must defer rather than
// succeed on a possibly partial foreign resolved set.
func NewRelationshipGenerationsCompleteLookup(
	checker RelationshipGenerationsCompleteChecker,
) maintenance.RelationshipGenerationsCompleteLookup {
	return func() (bool, error) {
		return checker.AreActiveScopeRelationshipGenerationsComplete(context.Background())
	}
}

// NewAcceptedGenerationLookup creates an exact bounded-unit acceptance lookup
// backed by shared_projection_acceptance.
func NewAcceptedGenerationLookup(db ExecQueryer) reducer.AcceptedGenerationLookup {
	store := NewSharedProjectionAcceptanceStore(db)
	return func(key reducer.SharedProjectionAcceptanceKey) (string, bool) {
		generationID, found, err := store.Lookup(
			context.Background(),
			key.ScopeID,
			key.AcceptanceUnitID,
			key.SourceRunID,
		)
		if err != nil {
			return "", false
		}
		return generationID, found
	}
}

// NewAcceptedGenerationPrefetch batches acceptance lookups for a current
// partition slice and returns an in-memory lookup closure for the reducer hot
// path. This keeps the shared runner collector-agnostic while avoiding repeated
// store calls for duplicate bounded-unit keys.
func NewAcceptedGenerationPrefetch(db ExecQueryer) reducer.AcceptedGenerationPrefetch {
	store := NewSharedProjectionAcceptanceStore(db)

	return func(ctx context.Context, intents []reducer.SharedProjectionIntentRow) (reducer.AcceptedGenerationLookup, error) {
		acceptedByKey := make(map[reducer.SharedProjectionAcceptanceKey]string, len(intents))

		for _, intent := range intents {
			key, ok := intent.AcceptanceKey()
			if !ok {
				continue
			}
			if _, seen := acceptedByKey[key]; seen {
				continue
			}

			generationID, found, err := store.Lookup(ctx, key.ScopeID, key.AcceptanceUnitID, key.SourceRunID)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			acceptedByKey[key] = generationID
		}

		return func(key reducer.SharedProjectionAcceptanceKey) (string, bool) {
			key.ScopeID = strings.TrimSpace(key.ScopeID)
			key.AcceptanceUnitID = strings.TrimSpace(key.AcceptanceUnitID)
			key.SourceRunID = strings.TrimSpace(key.SourceRunID)
			generationID, ok := acceptedByKey[key]
			return generationID, ok
		}, nil
	}
}

// AreActiveScopeRelationshipGenerationsComplete reports whether every active
// scope's current relationship generation is active. It backs the workload and
// deployable-unit correlation input gates so derivation that merges foreign
// resolved reads defers until the corpus-wide resolved set is complete
// (#6184). A single boolean row always returns; a missing row is impossible
// from the NOT EXISTS shape, and a query error fails safe as incomplete.
func (s *RelationshipStore) AreActiveScopeRelationshipGenerationsComplete(
	ctx context.Context,
) (bool, error) {
	rows, err := s.db.QueryContext(ctx, activeScopeRelationshipGenerationsCompleteSQL)
	if err != nil {
		return false, fmt.Errorf("query active scope relationship generations complete: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, fmt.Errorf("active scope relationship generations complete: no rows: %w", rows.Err())
	}
	var complete bool
	if err := rows.Scan(&complete); err != nil {
		return false, fmt.Errorf("scan active scope relationship generations complete: %w", err)
	}
	return complete, rows.Err()
}
