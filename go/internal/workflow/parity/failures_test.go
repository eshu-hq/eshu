// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClassifyMapsProductionDeferral pins CodePaced to the production
// deferral type the runner backoff path consumes, wrapped or not. A
// same-message impostor must stay CodeFault so the comparator cannot hide
// a type the runner would fail on.
func TestClassifyMapsProductionDeferral(t *testing.T) {
	t.Parallel()
	deferral := &workflow.ClaimPacingDeferral{Signal: "domain", RetryAfter: time.Second}
	if got := Classify(deferral); got != CodePaced {
		t.Fatalf("Classify(deferral) = %q, want %q", got, CodePaced)
	}
	wrapped := fmt.Errorf("parity satellite ClaimNext: %w", deferral)
	if got := Classify(wrapped); got != CodePaced {
		t.Fatalf("Classify(wrapped deferral) = %q, want %q", got, CodePaced)
	}
	// Byte-identical message, different type: the mapping keys on the
	// production type, never on message text.
	impostor := errors.New(deferral.Error())
	if got := Classify(impostor); got != CodeFault {
		t.Fatalf("Classify(impostor) = %q, want %q", got, CodeFault)
	}
}

// TestTruncateCoversSatelliteTables pins the constant TRUNCATE statement
// ResetSatellite executes to the satelliteTables set, so the two cannot
// drift: every listed table is truncated, and no unlisted table is.
func TestTruncateCoversSatelliteTables(t *testing.T) {
	t.Parallel()
	const prefix = "TRUNCATE "
	const suffix = " RESTART IDENTITY CASCADE"
	if !strings.HasPrefix(truncateSatelliteTables, prefix) ||
		!strings.HasSuffix(truncateSatelliteTables, suffix) {
		t.Fatalf("truncate statement changed shape: %q", truncateSatelliteTables)
	}
	listed := strings.Split(strings.TrimSuffix(strings.TrimPrefix(
		truncateSatelliteTables, prefix), suffix), ", ")
	if len(listed) != len(satelliteTables) {
		t.Fatalf("truncate covers %d tables, satelliteTables holds %d",
			len(listed), len(satelliteTables))
	}
	for i := range satelliteTables {
		if listed[i] != satelliteTables[i] {
			t.Fatalf("truncate table %d = %q, satelliteTables[%d] = %q",
				i, listed[i], i, satelliteTables[i])
		}
	}
}

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
// replays across every terminal mutation (#9428, SATELLITE HOLDS): an
// identical replay of an already-applied complete, release, fail-retryable,
// or fail-terminal epoch acks on both legs, while a heartbeat after the
// complete and a replay after a reclaim stay rejected on both legs.
//
// The scenario claims four epochs up front and binds one op per epoch by
// claim order, never by item identity: rotation order is an implementation
// detail the protocol does not pin, and no event records an item id, so the
// record is identical whatever order the leg picks. Each phase leaves its
// item completed or failed-terminal, so every reclaim below meets exactly
// one pending item.
func duplicateReplayScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	s.SeedRun(ctx, backend)
	s.SeedItems(ctx, backend, 4)
	epochs := make([]workflow.ClaimMutation, 4)
	for i := range epochs {
		item, claim, issued, err := backend.ClaimNextEligible(
			ctx, s.Selector("owner-a", fmt.Sprintf("%s-claim-%d", s.RunID, i)), s.Now, s.Lease)
		if err != nil || !issued {
			s.T.Fatalf("%s: claim %d: issued=%v err=%v", backend.Name(), i, issued, err)
		}
		epochs[i] = s.Mutation(item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)
	}

	// Complete: live heartbeats replay ok, the identical second complete
	// acks the committed epoch, and a heartbeat after it stays rejected.
	out.Call("heartbeat-1", backend.HeartbeatClaim(ctx, epochs[0]))
	out.Call("heartbeat-2", backend.HeartbeatClaim(ctx, epochs[0]))
	out.Call("complete-1", backend.CompleteClaim(ctx, epochs[0]))
	out.Call("complete-2", backend.CompleteClaim(ctx, epochs[0]))
	out.Call("heartbeat-after-complete", backend.HeartbeatClaim(ctx, epochs[0]))

	// Release: the identical second release acks the released epoch. The
	// item is pending again, so reclaim it (the only pending item) and
	// complete the successor epoch to drain it.
	out.Call("release-1", backend.ReleaseClaim(ctx, epochs[1]))
	out.Call("release-2", backend.ReleaseClaim(ctx, epochs[1]))
	released, releasedClaim, issued, err := backend.ClaimNextEligible(
		ctx, s.Selector("owner-a", s.RunID+"-claim-release-reclaim"), s.Now, s.Lease)
	out.Record("release-reclaim issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: release reclaim: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Call("complete-after-release", backend.CompleteClaim(ctx, s.Mutation(
		released.WorkItemID, releasedClaim.ClaimID, releasedClaim.FencingToken, releasedClaim.OwnerID)))

	// Fail-retryable: the identical second failure acks the failed epoch.
	// Reclaim the pending item, prove the pre-reclaim epoch is stale now,
	// and complete the successor epoch to drain it.
	retryable := epochs[2]
	retryable.FailureClass = "collect_failure"
	retryable.FailureMessage = "synthetic probe failure"
	out.Call("fail-retryable-1", backend.FailClaimRetryable(ctx, retryable))
	out.Call("fail-retryable-2", backend.FailClaimRetryable(ctx, retryable))
	retried, retriedClaim, issued, err := backend.ClaimNextEligible(
		ctx, s.Selector("owner-a", s.RunID+"-claim-retryable-reclaim"), s.Now, s.Lease)
	out.Record("retryable-reclaim issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: retryable reclaim: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Call("fail-retryable-replay-after-reclaim", backend.FailClaimRetryable(ctx, retryable))
	out.Call("complete-after-retryable", backend.CompleteClaim(ctx, s.Mutation(
		retried.WorkItemID, retriedClaim.ClaimID, retriedClaim.FencingToken, retriedClaim.OwnerID)))

	// Fail-terminal: the identical second failure acks the parked epoch.
	// The item is failed-terminal, so nothing reclaims it.
	terminal := epochs[3]
	terminal.FailureClass = "collect_failure"
	terminal.FailureMessage = "synthetic terminal failure"
	out.Call("fail-terminal-1", backend.FailClaimTerminal(ctx, terminal))
	out.Call("fail-terminal-2", backend.FailClaimTerminal(ctx, terminal))

	drainScenario(s, ctx, backend, out)
	return out
}
