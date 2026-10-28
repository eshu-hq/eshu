// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file holds conformance slice 1: empty queue, admission replay,
// concurrent single-holder claim with monotonic fencing, and stale-fence
// rejection. Slice 2 adds lease expiry and reaping, fail dispositions
// with dead-letter, duplicate and ambiguous-timeout replay, pin survival,
// pacing and starvation bounds, and the throughput comparison.
//
// Every scenario drains what it seeds: it ends with no pending or active
// item from its run, and records a drained event. ClaimNext selects
// globally, so an undrained scenario would contaminate the next one and
// fail its drain assertion loudly instead of silently.

// scenario is one named differential case. It must seed, exercise, and
// drain identically on any Backend, recording only comparable outcomes.
// Every scenario compares with Outcome.Equal: a divergence is a finding
// to fix behind a tracking issue, never a comparator leniency (#9428).
type scenario struct {
	name string
	run  func(s *Suite, ctx context.Context, backend Backend) *Outcome
}

func emptyQueueScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	_, _, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-0"), s.Now, s.Lease)
	out.Record("claim issued=%v err=%s", issued, Classify(err))
	reaped, err := backend.ReapExpiredClaims(ctx, s.Now, 10, time.Minute)
	out.Call("reap", err)
	out.Record("reaped n=%d", len(reaped))
	transitioned, err := backend.ReconcileWorkflowRuns(ctx, s.Now)
	out.Call("reconcile", err)
	out.Record("transitioned=%d", transitioned)
	return out
}

func admitReplayScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	s.SeedRun(ctx, backend)
	if err := backend.CreateRun(ctx, workflow.Run{
		RunID: s.RunID, TriggerKind: workflow.TriggerKindSchedule,
		Status:    workflow.RunStatusCollectionActive,
		CreatedAt: s.Now, UpdatedAt: s.Now,
	}); err != nil {
		s.T.Fatalf("%s: recreate run: %v", backend.Name(), err)
	}
	out.Record("recreate-run=%s", Classify(nil))
	s.SeedItems(ctx, backend, 1)
	item := workflow.WorkItem{
		WorkItemID: s.ItemID(0), RunID: s.RunID,
		CollectorKind: "jira", CollectorInstanceID: "jira-primary",
		SourceSystem: "jira", ScopeID: "scope-" + s.ItemID(0),
		AcceptanceUnitID: "au-" + s.ItemID(0),
		SourceRunID:      "sr-" + s.ItemID(0), GenerationID: "gen-" + s.ItemID(0),
		Status:    workflow.WorkItemStatusPending,
		CreatedAt: s.Now, UpdatedAt: s.Now,
	}
	out.Call("re-enqueue", backend.EnqueueWorkItems(ctx, []workflow.WorkItem{item}))
	got, claim, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-1"), s.Now, s.Lease)
	out.Record("claim issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: claim after replay: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Record("item=%s token=%d owner=%s attempts=%d", got.WorkItemID, claim.FencingToken, claim.OwnerID, got.AttemptCount)
	out.Call("complete", backend.CompleteClaim(ctx, s.Mutation(got.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)))
	drainScenario(s, ctx, backend, out)
	return out
}

func concurrentClaimScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	s.SeedRun(ctx, backend)
	s.SeedItems(ctx, backend, 1)
	const racers = 8
	type result struct {
		item   workflow.WorkItem
		claim  workflow.Claim
		issued bool
		err    error
	}
	results := make([]result, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := fmt.Sprintf("owner-%d", i)
			item, claim, issued, err := backend.ClaimNextEligible(
				ctx, s.Selector(owner, fmt.Sprintf("%s-claim-%d", s.RunID, i)), s.Now, s.Lease)
			results[i] = result{item: item, claim: claim, issued: issued, err: err}
		}(i)
	}
	wg.Wait()
	holders := 0
	var winner result
	for _, r := range results {
		if r.err != nil {
			s.T.Fatalf("%s: concurrent claim err: %v", backend.Name(), r.err)
		}
		if r.issued {
			holders++
			winner = r
		}
	}
	out.Record("holders=%d", holders)
	if holders != 1 {
		s.T.Fatalf("%s: holders=%d, want exactly 1", backend.Name(), holders)
	}
	out.Call("release", backend.ReleaseClaim(ctx,
		s.Mutation(winner.item.WorkItemID, winner.claim.ClaimID, winner.claim.FencingToken, winner.claim.OwnerID)))
	next, second, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-next", s.RunID+"-claim-next"), s.Now, s.Lease)
	out.Record("reclaim issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: reclaim after release: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Record("token1=%d token2=%d increasing=%v",
		winner.claim.FencingToken, second.FencingToken, second.FencingToken > winner.claim.FencingToken)
	out.Call("complete", backend.CompleteClaim(ctx,
		s.Mutation(next.WorkItemID, second.ClaimID, second.FencingToken, second.OwnerID)))
	drainScenario(s, ctx, backend, out)
	return out
}

func staleFenceScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	s.SeedRun(ctx, backend)
	s.SeedItems(ctx, backend, 1)
	first, firstClaim, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-a"), s.Now, s.Lease)
	if err != nil || !issued {
		s.T.Fatalf("%s: initial claim: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Record("token1=%d", firstClaim.FencingToken)
	out.Call("heartbeat-live", backend.HeartbeatClaim(ctx,
		s.Mutation(first.WorkItemID, firstClaim.ClaimID, firstClaim.FencingToken, "owner-a")))
	out.Call("fail-retryable", backend.FailClaimRetryable(ctx,
		s.Mutation(first.WorkItemID, firstClaim.ClaimID, firstClaim.FencingToken, "owner-a")))
	_, secondClaim, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-b", s.RunID+"-claim-b"), s.Now, s.Lease)
	out.Record("reclaim issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: reclaim: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Record("token2=%d increasing=%v", secondClaim.FencingToken, secondClaim.FencingToken > firstClaim.FencingToken)
	stale := s.Mutation(first.WorkItemID, firstClaim.ClaimID, firstClaim.FencingToken, "owner-a")
	out.Call("heartbeat-stale", backend.HeartbeatClaim(ctx, stale))
	out.Call("complete-stale", backend.CompleteClaim(ctx, stale))
	out.Call("complete-live", backend.CompleteClaim(ctx, s.Mutation(
		first.WorkItemID, secondClaim.ClaimID, secondClaim.FencingToken, "owner-b")))
	drainScenario(s, ctx, backend, out)
	return out
}

// drainScenario asserts the scenario leaves nothing claimable behind.
func drainScenario(s *Suite, ctx context.Context, backend Backend, out *Outcome) {
	s.T.Helper()
	_, _, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-drain", s.RunID+"-claim-drain"), s.Now, s.Lease)
	if err != nil || issued {
		s.T.Fatalf("%s: drain: issued=%v err=%v, scenario left work behind", backend.Name(), issued, err)
	}
	out.Record("drained=%v", !issued && err == nil)
}

func sliceOne() []scenario {
	return []scenario{
		{name: "empty-queue", run: emptyQueueScenario},
		{name: "admit-replay", run: admitReplayScenario},
		{name: "concurrent-single-holder", run: concurrentClaimScenario},
		{name: "stale-fence", run: staleFenceScenario},
		{name: "lease-reaper", run: leaseReaperScenario},
		{name: "fail-dispositions", run: failDispositionsScenario},
		{name: "duplicate-replay", run: duplicateReplayScenario},
		{name: "starvation-bound", run: starvationBoundScenario},
	}
}

// TestParityConformance runs slice 1 against both legs and requires
// identical outcome records. Backend legs come from the environment:
// PARITY_INTREE_DSN feeds the in-tree store on its own database, and
// PARITY_SATELLITE_URL plus PARITY_SATELLITE_DSN feed the satellite leg
// (local flip container or dark deployment) on its own database.
func TestParityConformance(t *testing.T) {
	ctx := context.Background()
	inTree := openInTreeBackend(t, os.Getenv("PARITY_INTREE_DSN"))
	satelliteDSN := os.Getenv("PARITY_SATELLITE_DSN")
	satelliteURL := os.Getenv("PARITY_SATELLITE_URL")
	ResetSatellite(t, satelliteDSN)
	t.Cleanup(func() { ResetSatellite(t, satelliteDSN) })
	satellite := OpenSatellite(t, satelliteURL)

	for _, sc := range sliceOne() {
		t.Run(sc.name, func(t *testing.T) {
			suiteA := NewSuite(t, sc.name)
			outA := sc.run(suiteA, ctx, inTree)
			suiteB := NewSuite(t, sc.name)
			suiteB.Now = suiteA.Now
			outB := sc.run(suiteB, ctx, satellite)
			outA.Equal(t, sc.name, outB)
			ResetSatellite(t, satelliteDSN)
		})
	}
}

// TestParityPinGap pins the arbiter-noted contract gap in place: the
// in-tree store has no commit pin, so its leg reports ErrPinNotSupported
// while the satellite honors ValidateAndPinClaimForCommit. Slice 2 turns
// this recorded gap into the pin-survives-reaper differential.
func TestParityPinGap(t *testing.T) {
	ctx := context.Background()
	inTree := openInTreeBackend(t, os.Getenv("PARITY_INTREE_DSN"))
	suite := NewSuite(t, "pin-gap")
	_, err := inTree.PinForCommit(ctx, "claim-x", 1, "scope-x", "sr-x", "gen-x", suite.Now.Add(time.Hour))
	if !errors.Is(err, ErrPinNotSupported) {
		t.Fatalf("in-tree pin err = %v, want ErrPinNotSupported", err)
	}
}
