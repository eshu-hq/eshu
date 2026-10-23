// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// validateFactBoundary admits one input fact into the projection of
// generation. A fact must belong to the projected scope and to the projected
// generation, or, for a delta projected over its #7797 delta-base union, to
// the union base named by generation.DeltaUnionBaseGenerationID (#8779). Any
// other generation, including a base the load did not name, fails the whole
// projection before any write.
func validateFactBoundary(scopeValue scope.IngestionScope, generation scope.ScopeGeneration, fact facts.Envelope) error {
	if fact.ScopeID != scopeValue.ScopeID {
		return fmt.Errorf("fact %q scope_id %q does not match scope %q", fact.FactID, fact.ScopeID, scopeValue.ScopeID)
	}
	if fact.GenerationID == generation.GenerationID {
		return nil
	}
	if base := generation.DeltaUnionBaseGenerationID; base != "" {
		if fact.GenerationID == base {
			return nil
		}
		return fmt.Errorf("fact %q generation_id %q does not match generation %q or its union base %q", fact.FactID, fact.GenerationID, generation.GenerationID, base)
	}
	return fmt.Errorf("fact %q generation_id %q does not match generation %q", fact.FactID, fact.GenerationID, generation.GenerationID)
}

// generationOwnedFacts returns the input facts the projected generation owns.
// For a delta over a union base these are the delta's own rows: reducer
// intents derive from them alone, because the git collector withholds the
// whole-repository follow-ups and the value-flow marker on a delta, and a
// base row would fire them under the delta against handlers that load only
// the delta's facts (#8779). Canonical and content materialization still read
// every input fact. Without a union base every admitted fact is owned and the
// input is returned unchanged.
func generationOwnedFacts(generation scope.ScopeGeneration, inputFacts []facts.Envelope) []facts.Envelope {
	if generation.DeltaUnionBaseGenerationID == "" {
		return inputFacts
	}
	owned := make([]facts.Envelope, 0, len(inputFacts))
	for i := range inputFacts {
		if inputFacts[i].GenerationID == generation.GenerationID {
			owned = append(owned, inputFacts[i])
		}
	}
	return owned
}

// validateUnionScope fails a union projection whose own facts carry no
// delta_generation repository fact: the #7797 union_scope_mismatch shape,
// where the overlay ran additively with duplicate stable keys and no delta
// marker, so canonical projection would run a full retract over a doubled
// set. It runs before any write.
func validateUnionScope(generation scope.ScopeGeneration, owned []facts.Envelope) error {
	base := generation.DeltaUnionBaseGenerationID
	if base == "" || facts.ParseDeltaScope(owned).IsDelta {
		return nil
	}
	return fmt.Errorf("delta generation %q names union base %q but carries no delta_generation repository fact (union_scope_mismatch); refusing the additive overlay",
		generation.GenerationID, base)
}
