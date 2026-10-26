// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// grantDispatchLease performs a dispatch-time heartbeat and returns its
// mutation plus the exact lease the store just granted
// (ObservedAt + LeaseDuration). The claimed item can carry a lease granted
// minutes earlier while the claim waited; the extension satellite fails
// closed on a past deadline (eshu-hq/eshu#9205), so the caller must ship the
// returned lease to the satellite instead of the frozen claim-time snapshot.
// A heartbeat error means the grant is lost or fenced away: the caller must
// fail the claim instead of dispatching into certain expiry. Fencing stays
// enforced by the store, which rejects a heartbeat whose claim ID, fencing
// token, or owner no longer holds the item.
func (s ClaimedService) grantDispatchLease(
	ctx context.Context,
	item workflow.WorkItem,
	claim workflow.Claim,
) (workflow.ClaimMutation, time.Time, error) {
	mutation := s.claimMutation(item, claim)
	if err := s.ControlStore.HeartbeatClaim(ctx, mutation); err != nil {
		return mutation, time.Time{}, fmt.Errorf("dispatch heartbeat claimed %s work item: %w", s.claimedKindLabel(), err)
	}
	return mutation, mutation.ObservedAt.Add(mutation.LeaseDuration), nil
}
