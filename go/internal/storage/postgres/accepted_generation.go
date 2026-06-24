// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
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
) reducer.RelationshipGenerationActiveLookup {
	return func(generationID string) (bool, error) {
		generationID = strings.TrimSpace(generationID)
		if generationID == "" {
			return false, nil
		}
		return checker.IsGenerationActive(context.Background(), generationID)
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
