// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"os"
	"testing"
	"time"
)

// Conformance slice 2a: lease expiry with reaper reclaim, and the commit
// pin surviving the reaper until its deadline.
//
// The pin case runs on the satellite leg only: the in-tree store has no
// commit pin (TestParityPinGap records the gap), so there is no in-tree
// expectation to compare. The differential for the pin is satellite
// behavior against the protocol contract, plus the recorded gap.

func leaseReaperScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	s.SeedRun(ctx, backend)
	s.SeedItems(ctx, backend, 1)
	_, claim, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-1"), s.Now, s.Lease)
	out.Record("claim issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: claim: issued=%v err=%v", backend.Name(), issued, err)
	}
	pastLease := s.Now.Add(10 * time.Minute)
	reaped, err := backend.ReapExpiredClaims(ctx, pastLease, 10, time.Minute)
	out.Call("reap", err)
	if err != nil {
		s.T.Fatalf("%s: reap: %v", backend.Name(), err)
	}
	out.Record("reaped n=%d", len(reaped))
	if len(reaped) != 1 || reaped[0].ClaimID != claim.ClaimID {
		s.T.Fatalf("%s: reaped=%+v, want the expired claim", backend.Name(), reaped)
	}
	out.Record("reaped status=%s", reaped[0].Status)
	visible := pastLease.Add(2 * time.Minute)
	next, second, issued, err := backend.ClaimNextEligible(ctx, s.Selector("owner-b", s.RunID+"-claim-2"), visible, s.Lease)
	out.Record("reclaim issued=%v err=%s", issued, Classify(err))
	if err != nil || !issued {
		s.T.Fatalf("%s: reclaim: issued=%v err=%v", backend.Name(), issued, err)
	}
	out.Record("token1=%d token2=%d increasing=%v",
		claim.FencingToken, second.FencingToken, second.FencingToken > claim.FencingToken)
	out.Call("complete", backend.CompleteClaim(ctx,
		s.Mutation(next.WorkItemID, second.ClaimID, second.FencingToken, second.OwnerID)))
	drainScenario(s, ctx, backend, out)
	return out
}

// TestParityPinSurvivesReaper proves a pinned claim survives the reaper
// until its deadline on the satellite leg. The in-tree leg has no pin to
// exercise; its gap stands recorded in TestParityPinGap.
func TestParityPinSurvivesReaper(t *testing.T) {
	ctx := context.Background()
	satellite := OpenSatellite(t, os.Getenv("PARITY_SATELLITE_URL"))
	satelliteDSN := os.Getenv("PARITY_SATELLITE_DSN")
	ResetSatellite(t, satelliteDSN)
	t.Cleanup(func() { ResetSatellite(t, satelliteDSN) })

	s := NewSuite(t, "pin-reaper")
	s.SeedRun(ctx, satellite)
	s.SeedItems(ctx, satellite, 1)
	item, claim, issued, err := satellite.ClaimNextEligible(ctx, s.Selector("owner-a", s.RunID+"-claim-1"), s.Now, s.Lease)
	if err != nil || !issued {
		t.Fatalf("satellite: claim: issued=%v err=%v", issued, err)
	}
	commitID, err := satellite.PinForCommit(ctx, claim.ClaimID, claim.FencingToken,
		"scope-"+item.WorkItemID, "sr-"+item.WorkItemID, "gen-"+item.WorkItemID, s.Now.Add(time.Hour))
	if err != nil {
		t.Fatalf("satellite: pin live claim: %v", err)
	}
	if commitID == "" {
		t.Fatalf("satellite: pin returned an empty commit id")
	}
	reaped, err := satellite.ReapExpiredClaims(ctx, s.Now.Add(10*time.Minute), 10, time.Minute)
	if err != nil {
		t.Fatalf("satellite: reap past lease: %v", err)
	}
	if len(reaped) != 0 {
		t.Fatalf("satellite: reaped=%+v, want nothing while the pin covers the claim", reaped)
	}
	if err := satellite.HeartbeatClaim(ctx,
		s.Mutation(item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)); err != nil {
		t.Fatalf("satellite: heartbeat pinned claim: %v", err)
	}
	// Past the deadline the pin no longer covers the claim: with a lease
	// that also expired, the reaper may take it.
	reaped, err = satellite.ReapExpiredClaims(ctx, s.Now.Add(2*time.Hour), 10, time.Minute)
	if err != nil {
		t.Fatalf("satellite: reap past deadline: %v", err)
	}
	if len(reaped) != 1 || reaped[0].ClaimID != claim.ClaimID {
		t.Fatalf("satellite: reaped=%+v, want the claim once its pin and lease lapse", reaped)
	}
}
