// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	remoteclaim "github.com/eshu-hq/eshu/go/internal/collector/extensionhost/remote/claim"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Concurrency mix shaping for the cutover multi-claim differential.
const (
	// mixWorkers is the worker count and mixIters the per-worker iteration
	// cap. Workers stop early once the transition epochs have provably
	// contended with them; the cap only bounds a stalled run. The product
	// keeps the race-detector run in the seconds range.
	mixWorkers = 8
	mixIters   = 60
	// mixContendedCalls is the minimum observed worker decisions per registry
	// state before the test flips to the next one. Transitions are driven by
	// these counts, never by sleeps, so a faster box cannot outrun them.
	mixContendedCalls = 64
	// mixAbsentKind is a second core kind with no grant in any registry
	// state, so its outcome is deterministic under transitions.
	mixAbsentKind = "aws_resource_policy_permission"
)

// mixClosedReason reports whether a deny reason is in the closed set the
// hostile matrix pins (leaf 3): any reason outside it is a leak.
func mixClosedReason(reason component.GrantReason) bool {
	switch reason {
	case component.GrantReasonRevoked,
		component.GrantReasonExpired,
		component.GrantReasonScopeMismatch,
		component.GrantReasonSchemaNotCovered,
		component.GrantReasonNoMatchingGrant,
		component.GrantReasonGrantsUnreadable:
		return true
	default:
		return false
	}
}

// TestCutoverConcurrentClaimMix proves the worker-built cutover holds under
// a multi-claim worker-pool mix with mid-run grant transitions: every call
// returns a closed outcome (allow or a matrix deny reason, never a raw
// error or panic), every governed call observes exactly once with the
// pinned holder, and the quiesced final state agrees with the in-tree
// truth (eshu-hq/eshu#8877 leaf 4, test-only half; digest-bound live proof
// follows under a fresh window grant).
func TestCutoverConcurrentClaimMix(t *testing.T) {
	t.Parallel()

	if !facts.IsCoreFactKind(parityCoreKind) || !facts.IsCoreFactKind(mixAbsentKind) {
		t.Fatalf("mix kinds %q/%q must both be core-owned", parityCoreKind, mixAbsentKind)
	}
	home := t.TempDir()
	registry := component.NewRegistry(home)
	live := parityLiveGrant()
	if err := registry.RecordGrant(live); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	observer := &recordingGrantObserver{}
	cutover := parityCutover(t, home, observer)

	var failures atomic.Int64
	recordFailure := func(format string, args ...any) {
		t.Helper()
		failures.Add(1)
		t.Errorf(format, args...)
	}
	// waitFor polls the observer until cond holds over the snapshot or the
	// stall timeout expires. Transition flips below are driven by observed
	// worker progress, not sleeps, so the registry states genuinely contend
	// with live workers no matter how fast the box runs them.
	waitFor := func(what string, cond func([]component.GrantDecision) bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			if cond(observer.snapshot()) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: workers stalled", what)
			}
			time.Sleep(200 * time.Microsecond)
		}
	}
	countWhere := func(got []component.GrantDecision, wantReason component.GrantReason, wantAllowed bool) int {
		n := 0
		for _, decision := range got {
			if decision.Kind == parityCoreKind && decision.Allowed == wantAllowed && decision.Reason == wantReason {
				n++
			}
		}
		return n
	}
	var started, done atomic.Int64
	var stop atomic.Bool
	iters := make([]int64, mixWorkers)
	release := make(chan struct{})
	worker := func(idx int) {
		defer done.Add(1)
		started.Add(1)
		for started.Load() < mixWorkers {
			time.Sleep(time.Millisecond)
		}
		// The first revoke lands before release, so every pre-restore call
		// provably runs under the revoked registry state.
		<-release
		ctx := context.Background()
		for i := 0; i < mixIters && !stop.Load(); i++ {
			iters[idx] = int64(i + 1)
			// Op A: state-dependent closed outcome on the granted kind.
			if err := cutover.AuthorizeEmission(ctx, parityCoreKind, parityCoreSchema); err != nil {
				deny, ok := err.(*remoteclaim.DenyError)
				if !ok {
					recordFailure("AuthorizeEmission() error = %v (%T), want nil or *DenyError", err, err)
				} else if !mixClosedReason(deny.Reason) {
					recordFailure("AuthorizeEmission() reason = %q, want a closed matrix reason", deny.Reason)
				}
			}
			// Op B: deterministic deny on the grantless kind in every state.
			if err := cutover.AuthorizeEmission(ctx, mixAbsentKind, parityCoreSchema); err == nil {
				recordFailure("AuthorizeEmission(%q) = nil, want deny (no grant in any state)", mixAbsentKind)
			} else if deny, ok := err.(*remoteclaim.DenyError); !ok {
				recordFailure("AuthorizeEmission(%q) error = %v (%T), want *DenyError", mixAbsentKind, err, err)
			} else if deny.Reason != component.GrantReasonNoMatchingGrant {
				recordFailure("AuthorizeEmission(%q) reason = %q, want no_matching_grant", mixAbsentKind, deny.Reason)
			}
			// Op C: live re-read never fails on a readable home.
			if _, err := cutover.ReadLiveGrants(ctx); err != nil {
				recordFailure("ReadLiveGrants() error = %v, want nil", err)
			}
		}
	}
	var wg sync.WaitGroup
	for w := 0; w < mixWorkers; w++ {
		wg.Add(1)
		go func() { defer wg.Done(); worker(w) }()
	}
	// Transitions flip the shared registry mid-run: revoke, restore, revoke.
	// Outcomes may reflect either state per call; all must stay closed. Each
	// flip waits for observed worker progress in the current state, so the
	// revoked and restored windows provably contain worker calls.
	waitFor("workers ready", func(got []component.GrantDecision) bool {
		return started.Load() == mixWorkers
	})
	if err := registry.RevokeGrant(parityHolderID, parityHolderVersion, parityCoreKind, parityScope); err != nil {
		t.Fatalf("RevokeGrant() error = %v, want nil", err)
	}
	close(release)
	waitFor("revoked-state calls", func(got []component.GrantDecision) bool {
		return countWhere(got, component.GrantReasonRevoked, false) >= mixContendedCalls
	})
	if err := registry.RecordGrant(live); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	waitFor("restored-state calls", func(got []component.GrantDecision) bool {
		return countWhere(got, component.GrantReasonGranted, true) >= mixContendedCalls
	})
	if err := registry.RevokeGrant(parityHolderID, parityHolderVersion, parityCoreKind, parityScope); err != nil {
		t.Fatalf("RevokeGrant() error = %v, want nil", err)
	}
	waitFor("second revoked-state calls", func(got []component.GrantDecision) bool {
		return countWhere(got, component.GrantReasonRevoked, false) >= 2*mixContendedCalls
	})
	if err := registry.RecordGrant(live); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	stop.Store(true)
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("mix recorded %d worker failures", failures.Load())
	}

	// Accounting: every governed emission observes exactly once. Iteration
	// counts are bounded by mixIters per worker and read after wg.Wait, so
	// the expected total is exact despite the dynamic stop.
	var totalIters int64
	for _, n := range iters {
		totalIters += n
	}
	wantDecisions := totalIters * 2
	got := observer.snapshot()
	if len(got) != int(wantDecisions) {
		t.Fatalf("observed decisions = %d, want %d (one per governed call)", len(got), wantDecisions)
	}
	if len(got) < 3*mixContendedCalls {
		t.Fatalf("observed decisions = %d, want a real contended mix", len(got))
	}
	// Overlap proof: the mix must have observed both registry states
	// mid-run, otherwise the transitions never contended with workers.
	var sawGranted, sawRevoked bool
	for _, decision := range got {
		if decision.Kind != parityCoreKind || decision.Allowed {
			if decision.Allowed {
				sawGranted = true
			}
			continue
		}
		if decision.Reason == component.GrantReasonRevoked {
			sawRevoked = true
		}
	}
	if !sawGranted || !sawRevoked {
		t.Fatalf("granted=%t revoked=%t: transitions never overlapped workers", sawGranted, sawRevoked)
	}
	for _, decision := range got {
		if decision.Stage != component.GrantStageEmission {
			t.Errorf("observed stage = %q, want emission", decision.Stage)
		}
		if decision.ProducerID != parityHolderID || decision.Version != parityHolderVersion {
			t.Errorf("observed holder = %q %q, want the pinned holder", decision.ProducerID, decision.Version)
		}
		if decision.Allowed {
			if decision.Reason != component.GrantReasonGranted {
				t.Errorf("observed allow reason = %q, want granted", decision.Reason)
			}
			continue
		}
		if !mixClosedReason(decision.Reason) {
			t.Errorf("observed deny reason = %q, want a closed matrix reason", decision.Reason)
		}
	}

	// Post-quiesce agreement: the restored live registry authorizes on the
	// cutover exactly as the in-tree predicates say.
	restored, readErr := component.NewRegistry(home).ProducerGrants()
	if readErr != nil {
		t.Fatalf("ProducerGrants() error = %v, want nil", readErr)
	}
	now := time.Now().UTC()
	inAllowed, inReason, _ := parityInTree(restored, nil, parityCoreKind, parityCoreSchema, now)
	if err := cutover.AuthorizeEmission(context.Background(), parityCoreKind, parityCoreSchema); (err == nil) != inAllowed {
		t.Fatalf("post-quiesce cutover ok=%t in-tree ok=%t: paths disagree", err == nil, inAllowed)
	} else if err == nil && inReason != component.GrantReasonGranted {
		t.Fatalf("post-quiesce in-tree reason = %q, want granted", inReason)
	}
}
