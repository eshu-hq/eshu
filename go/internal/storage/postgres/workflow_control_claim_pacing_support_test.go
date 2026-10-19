// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// pacingCleanupClaimSeq hands every cleanup grant a unique claim id across
// all pacing proofs: completed claim rows persist, so a repeated id would
// collide on workflow_claims_pkey.
var pacingCleanupClaimSeq atomic.Int64

// claimDepthPacingCounter consults CheckClaimPacing for one drain and counts
// the grants it paces. The hook honors the verdict the way the production
// runner does: a paced grant waits out the grant delay before claiming, so a
// paced drain proves delayed-but-complete rather than stranding work.
type claimDepthPacingCounter struct {
	t     *testing.T
	store *WorkflowControlStore
	cfg   workflow.ClaimPacingConfig
	waits atomic.Int64
}

// claimDepthPacingConsult builds the pacing hook for one drain. The idle
// (never-pacing) drain fails the test on the first paced verdict, so the
// anti-serialization floor cannot pass while pacing silently throttles.
func claimDepthPacingConsult(t *testing.T, store *WorkflowControlStore, cfg workflow.ClaimPacingConfig) *claimDepthPacingCounter {
	t.Helper()
	return &claimDepthPacingCounter{t: t, store: store, cfg: cfg}
}

func (c *claimDepthPacingCounter) hook(
	claimStore *WorkflowControlStore,
	selector workflow.ClaimSelector,
	now time.Time,
) time.Duration {
	decision, err := claimStore.CheckClaimPacing(context.Background(), selector, c.cfg, now)
	if err != nil {
		c.t.Errorf("CheckClaimPacing() error = %v, want nil (fail-open still reports the failure)", err)
		return 0
	}
	if !decision.Paced {
		return 0
	}
	c.waits.Add(1)
	return decision.RetryAfter
}

func (c *claimDepthPacingCounter) count() int64 {
	return c.waits.Load()
}

// completeClaimDepthItems claims and completes exactly n pending grants for
// one owner, shrinking the seeded backlog by n so pacing proofs can walk
// depth across the engage and release watermarks.
func completeClaimDepthItems(t *testing.T, store *WorkflowControlStore, ctx context.Context, owner string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "claim-depth-instance",
			OwnerID:             owner,
			ClaimID:             fmt.Sprintf("pacing-drain-cleanup-%d", pacingCleanupClaimSeq.Add(1)),
		}, time.Now().UTC(), 5*time.Minute)
		if err != nil {
			t.Fatalf("cleanup ClaimNextEligible() error = %v, want nil", err)
		}
		if !found {
			t.Fatalf("cleanup ClaimNextEligible() found = false after %d of %d completions, want pending work", i, n)
		}
		if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
			WorkItemID:   item.WorkItemID,
			ClaimID:      claim.ClaimID,
			FencingToken: claim.FencingToken,
			OwnerID:      claim.OwnerID,
			ObservedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("cleanup CompleteClaim(%q) error = %v, want nil", item.WorkItemID, err)
		}
	}
}
