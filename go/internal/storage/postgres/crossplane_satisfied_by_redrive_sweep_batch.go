// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

// SweepBatch is the startup/periodic catch-up path (issue #5476 P1-a): it
// reclaims up to limit XRD generation sweeps left 'queued' or whose claim
// lease expired (a crash or transient error mid-fan-out during a live-trigger
// Sweep call), then re-runs the fan-out for each. A per-claim error is
// recorded in the returned result slice's trailing error rather than
// aborting the whole batch, so one stuck generation cannot starve every
// other reclaimed row in the same pass. limit <= 0 defaults to 50.
func (s CrossplaneSatisfiedByRedriveSweeper) SweepBatch(
	ctx context.Context,
	limit int,
) ([]CrossplaneRedriveSweepResult, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = crossplaneRedriveDefaultCatchUpBatchSize
	}

	claims, err := s.State.ClaimBatch(ctx, s.Owner, s.leaseDuration(), limit)
	if err != nil {
		return nil, err
	}

	results := make([]CrossplaneRedriveSweepResult, 0, len(claims))
	var firstErr error
	for _, claim := range claims {
		// Re-derive the join keys fresh for each claimed generation: a batch
		// claim can be stale enough that the XRD generation has since been
		// superseded (fence (a)), in which case keys is empty and
		// runClaimedFanOut below resolves straight to MarkCompleted with no
		// fan-out -- the row is ALREADY claimed here (unlike Sweep's
		// pre-claim check), so it must be resolved, not left claimed again.
		keys, keyErr := s.loadActiveXRDJoinKeys(ctx, claim.XRDScopeID, claim.XRDGenerationID)
		if keyErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("catch-up sweep %s/%s: %w", claim.XRDScopeID, claim.XRDGenerationID, keyErr)
			}
			continue
		}
		result, sweepErr := s.runClaimedFanOut(ctx, claim.XRDScopeID, claim.XRDGenerationID, claim.FencingToken, keys)
		if sweepErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("catch-up sweep %s/%s: %w", claim.XRDScopeID, claim.XRDGenerationID, sweepErr)
			}
			continue
		}
		results = append(results, result)
	}
	return results, firstErr
}

// runClaimedFanOut runs the fan-out for an ALREADY-claimed XRD generation
// (via ClaimExact or ClaimBatch) and marks it complete, fenced by
// fencingToken. Shared by Sweep (the live post-activation trigger) and
// SweepBatch (the catch-up recovery path) so both converge on identical
// fencing and completion semantics.
func (s CrossplaneSatisfiedByRedriveSweeper) runClaimedFanOut(
	ctx context.Context,
	xrdScopeID string,
	xrdGenerationID string,
	fencingToken int64,
	keys []crossplaneRedriveXRDJoinKey,
) (CrossplaneRedriveSweepResult, error) {
	result := CrossplaneRedriveSweepResult{Attempted: true}
	seenTargets := make(map[string]struct{})
	for _, key := range keys {
		superseded, sweepErr := s.sweepJoinKey(ctx, xrdScopeID, xrdGenerationID, key, seenTargets, &result)
		if sweepErr != nil {
			return result, sweepErr
		}
		if superseded {
			break
		}
	}
	result.TargetsEnqueued = len(seenTargets)

	completed, err := s.State.MarkCompleted(ctx, xrdScopeID, xrdGenerationID, fencingToken)
	if err != nil {
		return result, err
	}
	if completed {
		result.Outcome = crossplaneRedriveOutcomeCompleted
	} else {
		// This invocation's lease expired mid-sweep and another invocation
		// reclaimed the row (bumping the fencing token); the fan-out work
		// already done here is safe (idempotent) but completion recording
		// belongs to whichever invocation currently holds the claim.
		result.Outcome = crossplaneRedriveOutcomeReclaimedMidSweep
	}
	s.recordSweep(ctx, result.Outcome)
	s.recordTargetsAndPages(ctx, result.TargetsEnqueued, result.PagesProcessed)
	return result, nil
}
