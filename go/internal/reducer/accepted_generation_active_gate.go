// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
)

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
//
// The activation fence applies ONLY to source runs whose generation IDs are
// relationship generation IDs (cross-repo resolver: "repo_dependency" or
// "repo_dependency:<scope>"). Code-import and package-consumption source runs
// carry scope generation IDs that never appear in relationship_generations, so
// applying the fence to them would permanently block those intents (B-13).
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
		if !requiresRelationshipGenerationGate(key.SourceRunID) {
			return generationID, true
		}
		active, err := isActive(generationID)
		if err != nil || !active {
			return "", false
		}
		return generationID, true
	}
}

// requiresRelationshipGenerationGate reports whether a repo-dependency
// source-run ID belongs to the cross-repo resolution path and therefore
// carries a relationship generation ID (an ID from relationship_generations).
//
// Only the cross-repo resolver emits source runs matching "repo_dependency" or
// "repo_dependency:<scopeID>". Code-import ("code_import_repo_dependency[:<s>]")
// and package-consumption ("package_consumption_repo_dependency[:<s>]") paths
// carry scope generation IDs from scope_generations, which are a separate table
// and will never be found in relationship_generations. Applying the activation
// gate to those paths permanently blocks their intents (B-13).
//
// Invariant for future sub-runners: any source run that carries a
// relationship generation ID (one written to relationship_generations by
// ActivateResolutionGeneration or CreateGeneration/CommitGeneration) MUST
// match this predicate — extend it if a new relationship-gen-backed path is
// added. Conversely, any source run matching "repo_dependency[:<scope>]" MUST
// carry a relationship generation ID; a new non-relationship path MUST NOT
// adopt this prefix.
func requiresRelationshipGenerationGate(sourceRunID string) bool {
	return sourceRunID == "repo_dependency" ||
		strings.HasPrefix(sourceRunID, "repo_dependency:")
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
