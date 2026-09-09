// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// loadResolvedRelationshipsForIntent reads the generation-pinned own-scope
// resolved set when the loader supports it, falling back to the scope read.
// It lives with the readiness gate because that gate exists to keep this
// read honest: callers must establish ownResolutionGenerationReady before
// calling it, never treat its output as complete on an inactive generation.
func loadResolvedRelationshipsForIntent(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	intent Intent,
) ([]relationships.ResolvedRelationship, error) {
	if generationScoped, ok := loader.(GenerationScopedResolvedRelationshipLoader); ok {
		return generationScoped.GetResolvedRelationshipsForGeneration(
			ctx,
			intent.ScopeID,
			intent.GenerationID,
		)
	}
	return loader.GetResolvedRelationships(ctx, intent.ScopeID)
}

// DeployableUnitCorrelationResolutionNotReadyFailureClass classifies a
// deferral of deployable-unit correlation work whose own relationship
// generation has not activated yet.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go) for the same
// reason as its #5717 sibling: the work is waiting on an upstream phase
// (relationship generation activation) rather than failing on its own merits,
// so charging it against the retry budget would dead-letter a still-pending
// scope that the succeeded-only reopen path would never reopen.
const DeployableUnitCorrelationResolutionNotReadyFailureClass = "deployable_unit_correlation_resolution_not_ready"

// deployableUnitCorrelationResolutionNotReadyError defers the intent until its
// own relationship generation activates.
type deployableUnitCorrelationResolutionNotReadyError struct {
	scopeID      string
	generationID string
}

func (e deployableUnitCorrelationResolutionNotReadyError) Error() string {
	return fmt.Sprintf(
		"cross-repo resolution not active for scope %s generation %s; deferring deployable unit correlation rather than evaluating against a partial resolved set",
		e.scopeID,
		e.generationID,
	)
}

func (deployableUnitCorrelationResolutionNotReadyError) Retryable() bool { return true }

func (deployableUnitCorrelationResolutionNotReadyError) FailureClass() string {
	return DeployableUnitCorrelationResolutionNotReadyFailureClass
}

// resolutionGenerationReady reports whether the relationship generation has
// activated. A nil lookup keeps the gate open for test wiring, matching
// canonicalNodesReady. A lookup error counts as not ready (fail safe): a
// transient failure must defer the intent, never wave it through.
func resolutionGenerationReady(
	lookup maintenance.RelationshipGenerationActiveLookup,
	generationID string,
) bool {
	if lookup == nil {
		return true
	}
	active, err := lookup(generationID)
	return err == nil && active
}

// ownResolutionGenerationReady reports whether the intent may proceed to its
// resolved-relationship read: its own relationship generation must be active.
// An inactive own generation means both feeds of that read are partial -- the
// generation-pinned own-scope load has no complete output yet, and the
// by-repos load over active generations has none of this scope's rows -- and
// a success on that partial input is never reopened (#6184), so the intent
// defers instead.
//
// Deliberately own-scope only. Foreign scopes feeding the by-repos read are
// undiscoverable before the read itself: candidate repo links attach from
// resolved rows, so gating on them would be circular, and an unresolvable
// foreign repo would hang the scope forever. Cross-scope completeness is the
// resolution layer's contract (it must not activate on partial evidence);
// this gate is the fail-closed floor beneath it. An empty candidate set skips
// the gate: there is nothing to evaluate, so holding the vacuous
// retract-and-publish path on resolution would stall content-free scopes on
// work they do not need.
func ownResolutionGenerationReady(
	lookup maintenance.RelationshipGenerationActiveLookup,
	intent Intent,
	candidates []WorkloadCandidate,
) bool {
	if len(candidates) == 0 {
		return true
	}
	return resolutionGenerationReady(lookup, intent.GenerationID)
}
