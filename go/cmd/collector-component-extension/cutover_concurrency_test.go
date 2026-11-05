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
	// mixWorkers is the worker count and mixIters the per-worker iterations.
	// The product keeps the race-detector run in the seconds range.
	mixWorkers = 8
	mixIters   = 60
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
	var started, done atomic.Int64
	worker := func() {
		defer done.Add(1)
		started.Add(1)
		for started.Load() < mixWorkers {
			time.Sleep(time.Millisecond)
		}
		ctx := context.Background()
		for i := 0; i < mixIters; i++ {
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
		go func() { defer wg.Done(); worker() }()
	}
	// Transitions flip the shared registry mid-run: revoke, restore, revoke.
	// Outcomes may reflect either state per call; all must stay closed.
	for f := 0; f < 3; f++ {
		time.Sleep(5 * time.Millisecond)
		if err := registry.RevokeGrant(parityHolderID, parityHolderVersion, parityCoreKind, parityScope); err != nil {
			t.Fatalf("RevokeGrant() error = %v, want nil", err)
		}
		time.Sleep(5 * time.Millisecond)
		if err := registry.RecordGrant(live); err != nil {
			t.Fatalf("RecordGrant() error = %v, want nil", err)
		}
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("mix recorded %d worker failures", failures.Load())
	}

	// Accounting: every governed emission observes exactly once.
	const wantDecisions = mixWorkers * mixIters * 2
	got := observer.snapshot()
	if len(got) != wantDecisions {
		t.Fatalf("observed decisions = %d, want %d (one per governed call)", len(got), wantDecisions)
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
