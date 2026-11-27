// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

type resolutionClaim struct {
	workItemID string
	claimedAt  time.Time
}

// ResolutionPersister persists resolution outputs (candidates and resolved
// relationships) for audit trail and activates the generation so downstream
// consumers (e.g. workload materialization) can query resolved relationships.
type ResolutionPersister interface {
	UpsertCandidates(ctx context.Context, generationID string, candidates []relationships.Candidate) error
	UpsertResolved(
		ctx context.Context,
		generationID string,
		resolved []relationships.ResolvedRelationship,
	) (ResolvedUpsertResult, error)
	ActivateResolutionGeneration(ctx context.Context, generationID, scopeID string) error
}

// ClaimFencedResolutionPersister publishes a generation only while the exact
// reducer claim that computed it remains live.
type ClaimFencedResolutionPersister interface {
	ActivateResolutionGenerationForClaim(
		ctx context.Context,
		generationID string,
		scopeID string,
		workItemID string,
		claimedAt time.Time,
	) error
}

// Resolve executes the cross-repo relationship resolution pipeline for one
// generation. Returns the number of durable intents emitted.
func (h *CrossRepoRelationshipHandler) Resolve(
	ctx context.Context,
	scopeID string,
	generationID string,
) (int, error) {
	writes, _, err := h.resolve(ctx, scopeID, generationID, nil)
	return writes, err
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
	writes, _, err := h.resolve(ctx, intent.ScopeID, intent.GenerationID, &resolutionClaim{
		workItemID: intent.IntentID,
		claimedAt:  intent.ClaimedAt.UTC(),
	})
	return writes, err
}

// ResolveWithDigest executes the same pipeline as Resolve and additionally
// reports the resolved-set digest: a stable hash of the ownership-partitioned
// resolved edges this pass published. A rerun over the same set computes the
// same digest, so downstream replay can stay quiet; a changed set moves the
// digest, so the replay reopens. It mirrors the RUNS_ON readiness fence,
// which hashes its sorted input set the same way.
func (h *CrossRepoRelationshipHandler) ResolveWithDigest(
	ctx context.Context,
	scopeID string,
	generationID string,
) (writes int, digest string, err error) {
	return h.resolve(ctx, scopeID, generationID, nil)
}

// ResolveClaimedWithDigest executes the same pipeline as ResolveClaimed and
// additionally reports the resolved-set digest (see ResolveWithDigest).
func (h *CrossRepoRelationshipHandler) ResolveClaimedWithDigest(
	ctx context.Context,
	intent reducercontract.Intent,
) (writes int, digest string, err error) {
	if intent.ClaimedAt == nil {
		return 0, "", fmt.Errorf("cross-repo resolution requires claimed_at: %w", reducercontract.ErrExecutionClaimRejected)
	}
	return h.resolve(ctx, intent.ScopeID, intent.GenerationID, &resolutionClaim{
		workItemID: intent.IntentID,
		claimedAt:  intent.ClaimedAt.UTC(),
	})
}

// resolvedSetDigest hashes the sorted durable identities of one pass's
// ownership-partitioned resolved edges. Sorting makes the digest independent
// of slice order, and ResolvedRelationshipID is already a pure function of
// the stored identity, so reruns over the same set converge. The empty set
// hashes to the stable digest of no input: the first empty pass replays once
// by fail-open, and its repeat stays quiet.
func resolvedSetDigest(generationID string, owned []relationships.ResolvedRelationship) string {
	inputs := make([]string, 0, len(owned))
	for _, r := range owned {
		inputs = append(inputs, relationships.ResolvedRelationshipID(generationID, r))
	}
	sort.Strings(inputs)
	sum := sha256.Sum256([]byte(strings.Join(inputs, "\x00")))
	return fmt.Sprintf("%x", sum[:])
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
