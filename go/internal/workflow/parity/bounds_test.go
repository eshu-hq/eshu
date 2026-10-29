// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Conformance slice 2c: starvation bound and throughput comparison.
//
// The starvation scenario seeds more items than one claim round can take
// and requires every item to complete within a bounded number of rounds
// with exactly one attempt each: no family may starve another. Only
// counts enter the outcome record, never pick order — rotation order is
// an implementation detail the protocol does not pin.
//
// The throughput comparison runs the same seeded workload on both legs
// and logs durations. It asserts functional completion only: the local
// satellite leg runs emulated (amd64 image on an arm64 host), so a
// latency ratio gate would measure the emulator, not the server. The
// binding no-regression comparison belongs to the dark-deployment run, where
// both images run native; the logged numbers here are its baseline.

func starvationBoundScenario(s *Suite, ctx context.Context, backend Backend) *Outcome {
	out := &Outcome{}
	const items = 10
	s.SeedRun(ctx, backend)
	s.SeedItems(ctx, backend, items)
	completed := 0
	attempts := 0
	rounds := 0
	for completed < items && rounds < 2*items {
		rounds++
		item, claim, issued, err := backend.ClaimNextEligible(
			ctx, s.Selector("owner-drain", fmt.Sprintf("%s-claim-%d", s.RunID, rounds)), s.Now, s.Lease)
		if err != nil {
			s.T.Fatalf("%s: round %d claim: %v", backend.Name(), rounds, err)
		}
		if !issued {
			break
		}
		attempts += item.AttemptCount
		if err := backend.CompleteClaim(ctx,
			s.Mutation(item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)); err != nil {
			s.T.Fatalf("%s: round %d complete: %v", backend.Name(), rounds, err)
		}
		completed++
	}
	out.Record("completed=%d attempts=%d rounds=%d", completed, attempts, rounds)
	if completed != items {
		s.T.Fatalf("%s: completed=%d, want %d (starvation)", backend.Name(), completed, items)
	}
	drainScenario(s, ctx, backend, out)
	return out
}

// TestParityThroughput runs the same seeded claim-and-complete workload
// on both legs and logs the comparison. Same seed, same shape, one
// number per leg for the dark-deployment baseline.
func TestParityThroughput(t *testing.T) {
	ctx := context.Background()
	inTree := openInTreeBackend(t, os.Getenv("PARITY_INTREE_DSN"))
	satelliteDSN := os.Getenv("PARITY_SATELLITE_DSN")
	satelliteURL := os.Getenv("PARITY_SATELLITE_URL")
	ResetSatellite(t, satelliteDSN)
	t.Cleanup(func() { ResetSatellite(t, satelliteDSN) })
	satellite := OpenSatellite(t, satelliteURL)

	const items = 30
	run := func(name string, backend Backend, seed func(*Suite)) time.Duration {
		// Identical fixtures on both legs: same run id, same items,
		// same clock. Legs hold separate databases, so ids never meet.
		s := NewSuite(t, "throughput")
		seed(s)
		start := time.Now()
		done := 0
		for done < items {
			item, claim, issued, err := backend.ClaimNextEligible(
				ctx, s.Selector("owner-t", fmt.Sprintf("%s-claim-%d", s.RunID, done)), s.Now, s.Lease)
			if err != nil || !issued {
				t.Fatalf("%s: claim %d: issued=%v err=%v", name, done, issued, err)
			}
			if err := backend.CompleteClaim(ctx,
				s.Mutation(item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)); err != nil {
				t.Fatalf("%s: complete %d: %v", name, done, err)
			}
			done++
		}
		elapsed := time.Since(start)
		t.Logf("%s: %d claim+complete in %s (%.1f ops/s)", name, items, elapsed, float64(2*items)/elapsed.Seconds())
		return elapsed
	}
	inTreeSuite := func(s *Suite) {
		s.SeedRun(ctx, inTree)
		s.SeedItems(ctx, inTree, items)
	}
	satSuite := func(s *Suite) {
		s.SeedRun(ctx, satellite)
		s.SeedItems(ctx, satellite, items)
	}
	inTreeElapsed := run("in-tree", inTree, inTreeSuite)
	ResetSatellite(t, satelliteDSN)
	satElapsed := run("satellite", satellite, satSuite)
	leg := "local satellite leg"
	if os.Getenv("PARITY_DARK") == "1" {
		leg = "native dark-deployment satellite leg"
	}
	t.Logf("parity throughput ratio satellite/in-tree: %.2f (%s)",
		float64(satElapsed)/float64(inTreeElapsed), leg)
}
