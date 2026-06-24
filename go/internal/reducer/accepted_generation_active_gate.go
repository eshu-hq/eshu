// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "context"

// RelationshipGenerationActiveLookup reports whether a relationship generation
// is currently active (published) in Postgres. It backs the repo-dependency
// graph-projection authority gate so the graph runner cannot project edges for
// a generation the Postgres relationship read models do not yet expose.
//
// A non-nil error is treated by the gate as "not active" (fail safe): a
// transient lookup failure must never let graph edges publish ahead of the
// Postgres generation swap.
type RelationshipGenerationActiveLookup func(generationID string) (bool, error)

// GateAcceptedGenerationOnActive decorates an AcceptedGenerationLookup so an
// accepted generation only grants graph-projection authority once it is also
// active (published) in Postgres.
//
// This closes the dual-write graph-ahead-of-Postgres window for the
// repo-dependency lane: shared_projection_acceptance rows are written atomically
// with the projection intents (durable acceptance), but the repo-dependency
// runner derives authority solely from those acceptance rows. Without this gate,
// committing acceptance before generation activation would let the graph runner
// write edges for an un-published generation while the Postgres relationship
// read models still filter on relationship_generations.status = 'active',
// leaving the graph ahead of Postgres until a later retry. Gating authority on
// activation makes activation the single fence that opens both the graph and the
// Postgres read-model surfaces together.
//
// When the base lookup has no acceptance row the gate defers without invoking
// the active check (no acceptance, no authority). When the active check errors
// or reports the generation inactive, the gate defers (returns no authority) so
// the runner retries on a later cycle rather than projecting ahead of Postgres.
//
// The #3559/#3616 reconciler remains as defense-in-depth for any residual drift.
func GateAcceptedGenerationOnActive(
	base AcceptedGenerationLookup,
	isActive RelationshipGenerationActiveLookup,
) AcceptedGenerationLookup {
	if base == nil || isActive == nil {
		return base
	}
	return func(key SharedProjectionAcceptanceKey) (string, bool) {
		generationID, ok := base(key)
		if !ok {
			return "", false
		}
		active, err := isActive(generationID)
		if err != nil || !active {
			return "", false
		}
		return generationID, true
	}
}

// GateAcceptedGenerationPrefetchOnActive applies the activation fence to the
// batched prefetch path the repo-dependency runner uses on its hot path, so the
// same fence holds whether authority is resolved per-key or via the prefetched
// in-memory lookup.
//
// The active-status check is memoized per resolved-lookup closure: the prefetch
// returns a fresh closure each cycle, and within one cycle a generation's
// active status is stable, so each distinct generation id is checked at most
// once. This keeps the gate from adding a Postgres round trip per intent row on
// the hot selection/filter path while still re-reading active status on the next
// cycle (a superseded generation reads inactive then).
func GateAcceptedGenerationPrefetchOnActive(
	base AcceptedGenerationPrefetch,
	isActive RelationshipGenerationActiveLookup,
) AcceptedGenerationPrefetch {
	if base == nil || isActive == nil {
		return base
	}
	return func(ctx context.Context, intents []SharedProjectionIntentRow) (AcceptedGenerationLookup, error) {
		resolved, err := base(ctx, intents)
		if err != nil {
			return nil, err
		}
		return GateAcceptedGenerationOnActive(resolved, memoizeActiveLookup(isActive)), nil
	}
}

// memoizeActiveLookup caches active-status results per generation id for the
// lifetime of one prefetched lookup closure (one runner cycle). It is not safe
// for concurrent use; the repo-dependency runner resolves authority on a single
// goroutine per cycle.
func memoizeActiveLookup(isActive RelationshipGenerationActiveLookup) RelationshipGenerationActiveLookup {
	cache := make(map[string]bool)
	return func(generationID string) (bool, error) {
		if active, ok := cache[generationID]; ok {
			return active, nil
		}
		active, err := isActive(generationID)
		if err != nil {
			return false, err
		}
		cache[generationID] = active
		return active, nil
	}
}
