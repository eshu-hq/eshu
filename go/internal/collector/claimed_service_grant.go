// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// claimNext acquires one grant for the runner: through the fair dispatcher
// when one is wired, otherwise directly from the control store for the
// service's own family. A paced grant returns the pacing deferral with no
// claim issued, so the runner backs off on the retry-after path.
func (s ClaimedService) claimNext(
	ctx context.Context,
	claimID string,
) (workflow.WorkItem, workflow.Claim, workflow.ClaimTarget, bool, error) {
	if s.ClaimDispatcher != nil {
		item, claim, target, found, err := s.ClaimDispatcher.ClaimNext(ctx, s.OwnerID, claimID, s.now(), s.ClaimLeaseTTL)
		if err != nil {
			return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false,
				fmt.Errorf("dispatch claim next: %w", err)
		}
		return item, claim, target, found, nil
	}
	target := workflow.ClaimTarget{
		CollectorKind:       s.CollectorKind,
		CollectorInstanceID: s.CollectorInstanceID,
	}
	selector := workflow.ClaimSelector{
		CollectorKind:       target.CollectorKind,
		CollectorInstanceID: target.CollectorInstanceID,
		OwnerID:             s.OwnerID,
		ClaimID:             claimID,
	}
	if deferral := s.pacingGrantDeferral(ctx, selector); deferral != nil {
		return workflow.WorkItem{}, workflow.Claim{}, target, false, deferral
	}
	item, claim, found, err := s.ControlStore.ClaimNextEligible(ctx, selector, s.now(), s.ClaimLeaseTTL)
	if err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, target, false,
			fmt.Errorf("claim next %s/%s work item: %w", target.CollectorKind, target.CollectorInstanceID, err)
	}
	return item, claim, target, found, nil
}

// pacingGrantDeferral consults depth pacing for the direct (undispatched)
// claim path. It returns nil when pacing is unconfigured, when the control
// store does not serve pacing consults, when the consult fails (fail open),
// or when the verdict is unpaced. A paced verdict returns the grant-delay
// deferral with no claim issued.
func (s ClaimedService) pacingGrantDeferral(
	ctx context.Context,
	selector workflow.ClaimSelector,
) *workflow.ClaimPacingDeferral {
	if s.ClaimPacing == nil || !s.ClaimPacing.Enabled() {
		return nil
	}
	checker, ok := s.ControlStore.(ClaimPacingChecker)
	if !ok || checker == nil {
		return nil
	}
	decision, err := checker.CheckClaimPacing(ctx, selector, *s.ClaimPacing, s.now())
	if err != nil {
		// A pacing consult failure fails open: an unavailable depth signal
		// is not evidence of a backlog, so the grant claims normally. The
		// debug record keeps the silence diagnosable.
		if s.Logger != nil {
			s.Logger.DebugContext(ctx, "pacing consult failed open",
				slog.String("domain", string(selector.CollectorKind)+"/"+selector.CollectorInstanceID),
				slog.String("error", err.Error()),
			)
		}
		return nil
	}
	if !decision.Paced {
		return nil
	}
	return &workflow.ClaimPacingDeferral{
		Signal:     decision.Signal,
		Domain:     decision.Domain,
		RetryAfter: decision.RetryAfter,
		Stuck:      decision.Stuck,
	}
}
