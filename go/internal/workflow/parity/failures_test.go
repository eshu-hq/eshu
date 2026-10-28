// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
)

// Conformance slice 2b: fail dispositions and duplicate delivery.
//
// FailClaimRetryable requeues the item for another attempt while
// FailClaimTerminal parks it: the queue goes idle with the item
// failed_terminal. Claim-level dead-letter counts surface on the report
// plane (run progress), which is coordinator surface outside this claim
// contract; the differential here covers dispositions and observable
// item state.

func failDispositionsScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	s.SeedRun(ctx, backend)
	s.SeedItems(ctx, backend, 2)
	first, firstClaim, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-1"), s.Now, s.Lease)
	if err != nil || !issued {
		s.T.Fatalf("%s: claim: issued=%v err=%v", backend.Name(), issued, err)
	}
	retryable := s.Mutation(first.WorkItemID, firstClaim.ClaimID, firstClaim.FencingToken, "owner-a")
	retryable.FailureClass = "collect_failure"
	retryable.FailureMessage = "synthetic probe failure"
	out.Call("fail-retryable", backend.FailClaimRetryable(ctx, retryable))
	again, againClaim, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-2"), s.Now, s.Lease)
	out.Record("reissue issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: reissue: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Record("same-item=%v token=%d attempts=%d",
		again.WorkItemID == first.WorkItemID, againClaim.FencingToken, again.AttemptCount)
	terminal := s.Mutation(again.WorkItemID, againClaim.ClaimID, againClaim.FencingToken, "owner-a")
	terminal.FailureClass = "collect_failure"
	terminal.FailureMessage = "synthetic terminal failure"
	out.Call("fail-terminal", backend.FailClaimTerminal(ctx, terminal))
	other, otherClaim, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-3"), s.Now, s.Lease)
	out.Record("next issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: second item claim: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Record("second-item=%s", other.WorkItemID)
	out.Call("complete", backend.CompleteClaim(ctx,
		s.Mutation(other.WorkItemID, otherClaim.ClaimID, otherClaim.FencingToken, otherClaim.OwnerID)))
	drainScenario(s, ctx, backend, out)
	return out
}

// duplicateReplayScenario proves idempotent handling of ambiguous-timeout
// replays: identical mutations repeat without corrupting state, and a
// second complete of a finished epoch is rejected on both legs.
func duplicateReplayScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	s.SeedRun(ctx, backend)
	s.SeedItems(ctx, backend, 1)
	item, claim, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-1"), s.Now, s.Lease)
	if err != nil || !issued {
		s.T.Fatalf("%s: claim: issued=%v err=%v", backend.Name(), issued, err)
	}
	mutation := s.Mutation(item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)
	out.Call("heartbeat-1", backend.HeartbeatClaim(ctx, mutation))
	out.Call("heartbeat-2", backend.HeartbeatClaim(ctx, mutation))
	out.Call("complete-1", backend.CompleteClaim(ctx, mutation))
	out.Call("complete-2", backend.CompleteClaim(ctx, mutation))
	drainScenario(s, ctx, backend, out)
	return out
}
