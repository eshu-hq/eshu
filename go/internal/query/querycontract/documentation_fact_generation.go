// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "fmt"

// DocumentationFactPageState is what a documentation facts page's generation
// binding, freshness, and empty reason resolve to once the store has read the
// scope or generation row that explains the page (#7128).
type DocumentationFactPageState struct {
	Binding     DocumentationFactGenerationBinding
	Freshness   DocumentationFactFreshness
	EmptyReason string
}

// DocumentationFactEmptyScopeState maps the ingestion_scopes row behind a
// zero-row scope read to its page state. found is false when no scope has the
// requested id. It never falls back to a latest generation: a scope with no
// active generation has no current facts, and serving an unadmitted or
// dead-lettered generation as exact truth would be wrong.
//
// active_generation_id is cleared only by the projector dead-letter
// transition, which also marks the scope failed, so a failed scope is
// dead_lettered_domain; any other scope without an active generation has not
// activated one yet.
func DocumentationFactEmptyScopeState(found bool, status, activeGenerationID string) DocumentationFactPageState {
	switch {
	case !found:
		return DocumentationFactPageState{EmptyReason: DocumentationFactEmptyScopeNotFound}
	case activeGenerationID == "":
		state := DocumentationFactPageState{
			EmptyReason: DocumentationFactEmptyNoActiveGeneration,
			Freshness: DocumentationFactFreshness{
				State:  FreshnessBuilding,
				Cause:  FreshnessCausePendingRepoGeneration,
				Detail: "scope has no active generation",
			},
		}
		if status == "failed" {
			state.Freshness.State = FreshnessUnavailable
			state.Freshness.Cause = FreshnessCauseDeadLetteredDomain
		}
		return state
	default:
		return DocumentationFactPageState{
			EmptyReason: DocumentationFactEmptyNoRows,
			Binding:     DocumentationFactGenerationBinding{GenerationID: activeGenerationID, IsActive: true},
		}
	}
}

// DocumentationFactExplicitGenerationState maps the scope_generations row of a
// caller-named generation to its page state, using the closed status
// vocabulary the freshness generations route publishes. found is false when no
// generation row has the id, or when it belongs to a scope other than the one
// the caller asked for; pruned and never-existed generations are
// indistinguishable, so no cause is claimed for them.
func DocumentationFactExplicitGenerationState(
	generationID string,
	found bool,
	generationScopeID, requestedScopeID, status string,
) DocumentationFactPageState {
	state := DocumentationFactPageState{
		Binding: DocumentationFactGenerationBinding{GenerationID: generationID},
	}
	if !found || (requestedScopeID != "" && requestedScopeID != generationScopeID) {
		state.Freshness = DocumentationFactFreshness{
			State:  FreshnessUnavailable,
			Detail: fmt.Sprintf("generation %s is not a known scope generation", generationID),
		}
		return state
	}
	switch status {
	case "active":
		state.Binding.IsActive = true
	case "superseded", "completed", "failed":
		// Truth that was correct when observed and has a known reason for
		// lagging; no closed cause fits, so none is claimed.
		state.Freshness = DocumentationFactFreshness{
			State:  FreshnessStale,
			Detail: fmt.Sprintf("generation %s is %s for scope %s", generationID, status, generationScopeID),
		}
	case "pending":
		state.Freshness = DocumentationFactFreshness{
			State:  FreshnessBuilding,
			Cause:  FreshnessCausePendingRepoGeneration,
			Detail: fmt.Sprintf("generation %s is pending", generationID),
		}
	default:
		state.Freshness = DocumentationFactFreshness{
			State:  FreshnessUnavailable,
			Detail: fmt.Sprintf("generation %s has unrecognized status %q", generationID, status),
		}
	}
	return state
}
