// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type resolutionClaim struct {
	workItemID string
	claimedAt  time.Time
}

// Resolve executes the cross-repo relationship resolution pipeline for one
// generation. Returns the number of durable intents emitted.
func (h *CrossRepoRelationshipHandler) Resolve(
	ctx context.Context,
	scopeID string,
	generationID string,
) (int, error) {
	return h.resolve(ctx, scopeID, generationID, nil)
}

// ResolveClaimed executes resolution with a publication fence bound to the
// exact durable reducer claim. Production reducer execution uses this entry
// point so recovery or lease takeover cannot be undone by a stale resolver.
func (h *CrossRepoRelationshipHandler) ResolveClaimed(
	ctx context.Context,
	intent reducercontract.Intent,
) (int, error) {
	if intent.ClaimedAt == nil {
		return 0, fmt.Errorf("cross-repo resolution requires claimed_at: %w", reducercontract.ErrExecutionClaimRejected)
	}
	return h.resolve(ctx, intent.ScopeID, intent.GenerationID, &resolutionClaim{
		workItemID: intent.IntentID,
		claimedAt:  intent.ClaimedAt.UTC(),
	})
}

func (h *CrossRepoRelationshipHandler) activateResolutionGeneration(
	ctx context.Context,
	generationID string,
	scopeID string,
	claim *resolutionClaim,
) error {
	if claim == nil {
		return h.Persister.ActivateResolutionGeneration(ctx, generationID, scopeID)
	}
	persister, ok := h.Persister.(ClaimFencedResolutionPersister)
	if !ok {
		return fmt.Errorf("resolution persister %T does not support claim-fenced activation", h.Persister)
	}
	return persister.ActivateResolutionGenerationForClaim(
		ctx,
		generationID,
		scopeID,
		claim.workItemID,
		claim.claimedAt,
	)
}
