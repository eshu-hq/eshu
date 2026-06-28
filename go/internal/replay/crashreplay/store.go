// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crashreplay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/clock"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// itemPhase is the durable lifecycle of one work item, modeling the
// fact_work_items row state the reducer queue persists in Postgres.
type itemPhase int

const (
	// phasePending: never claimed, or released back after a failure.
	phasePending itemPhase = iota
	// phaseClaimed: a worker holds a lease until leaseEnd. A crashed worker
	// leaves an item stuck here; it becomes reclaimable once leaseEnd lapses.
	phaseClaimed
	// phaseCompleted: terminal. A completed item is never claimed again — the
	// fencing guarantee that makes crash recovery idempotent.
	phaseCompleted
)

// itemState is the durable record for one work item: its lifecycle phase, its
// current lease deadline, and its attempt count. attempt is the fencing token —
// it increments on every (re)claim, so a reclaim after a crash is observably a
// new attempt, not a silent retry of the old one. completedOnce is a sticky flag
// (never cleared by a re-claim) so a second completion of the same item is
// detectable even if a fencing bug re-handed it and flipped its phase back to
// claimed.
type itemState struct {
	intent        reducer.Intent
	phase         itemPhase
	leaseEnd      time.Time
	attempt       int
	completedOnce bool
}

// durableStore is the in-memory stand-in for the durable Postgres
// fact_work_items + lease state the reducer queue claims from. It is the only
// state that survives a simulated crash: the harness drops the in-memory worker
// (the reducer service goroutine) but keeps this store, exactly as a real crash
// keeps committed Postgres rows while losing process memory.
//
// Claim hands out the first pending item, or the first item whose lease has
// expired on the injected clock — the recovery path. Completed items are never
// re-handed-out, which is the fencing guarantee R-14 asserts. All methods are
// safe for concurrent use so the store can back the reducer worker pool, though
// crash injection itself runs single-worker for determinism.
//
// It is unexported because nothing outside this package consumes it directly;
// the package surface is Config / RunToCompletion / RunWithCrash. Claim, Ack,
// and Fail are exported only because they satisfy reducer.WorkSource and
// reducer.WorkSink, whose interface method names are exported.
type durableStore struct {
	mu       sync.Mutex
	clk      clock.Clock
	leaseTTL time.Duration
	order    []string
	items    map[string]*itemState

	// Durable observability counters. They survive the crash with the store, so
	// the recovery report can prove what happened across both phases.
	acks       int
	doubleAcks int
	reclaims   int
	maxAttempt int
}

// newDurableStore builds a durable store seeded with one record per work item,
// in the given delivery order. It fails loudly on a duplicate IntentID: crash
// recovery keys on a 1:1 durable record per item, so a duplicate schedule would
// make "completed" ambiguous. clk drives lease expiry and seeds the enqueue
// timestamp (so the simulated-clock anchor is the single source of time);
// leaseTTL is how long a claim holds its lease before a crashed item becomes
// reclaimable.
func newDurableStore(items []schedulereplay.WorkItem, clk clock.Clock, leaseTTL time.Duration) (*durableStore, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("crashreplay: no work items to schedule")
	}
	if clk == nil {
		return nil, fmt.Errorf("crashreplay: clock is required")
	}
	if leaseTTL <= 0 {
		return nil, fmt.Errorf("crashreplay: lease TTL must be positive, got %s", leaseTTL)
	}
	enqueued := clk.Now()
	store := &durableStore{
		clk:      clk,
		leaseTTL: leaseTTL,
		order:    make([]string, 0, len(items)),
		items:    make(map[string]*itemState, len(items)),
	}
	for _, item := range items {
		if _, exists := store.items[item.IntentID]; exists {
			return nil, fmt.Errorf("crashreplay: duplicate intent id %q in schedule", item.IntentID)
		}
		store.order = append(store.order, item.IntentID)
		store.items[item.IntentID] = &itemState{
			intent: reducer.Intent{
				IntentID:     item.IntentID,
				ScopeID:      "replay-crash",
				GenerationID: "replay-gen",
				SourceSystem: "replay",
				Domain:       reducer.DomainCodeCallMaterialization,
				Cause:        "crash-replay",
				Status:       reducer.IntentStatusPending,
				EnqueuedAt:   enqueued,
				AvailableAt:  enqueued,
			},
		}
	}
	return store, nil
}

// Claim returns the next claimable intent, or ok=false when none is currently
// claimable. An item is claimable when it is pending or when its lease has
// lapsed on the injected clock (the crash-recovery path). Claiming sets a fresh
// lease and increments the fencing token (attempt count); the returned intent
// carries that attempt count. Completed items are never returned — the fencing
// guarantee.
func (s *durableStore) Claim(_ context.Context) (reducer.Intent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clk.Now()
	for _, id := range s.order {
		st := s.items[id]
		switch {
		case st.phase == phaseCompleted:
			// Fencing guarantee: a completed item must never be re-handed, which
			// would re-project finished work.
			continue
		case st.phase == phasePending:
			// First claim; fall through to the claim below.
		case st.phase == phaseClaimed && !st.leaseEnd.After(now):
			// The prior lease lapsed (a crashed or stalled holder) and we are
			// taking it over: that is a reclaim, not a first claim.
			s.reclaims++
		default:
			// Claimed with a still-live lease: not claimable.
			continue
		}
		st.phase = phaseClaimed
		st.attempt++
		st.leaseEnd = now.Add(s.leaseTTL)
		if st.attempt > s.maxAttempt {
			s.maxAttempt = st.attempt
		}
		claimedAt := now
		intent := st.intent
		intent.Status = reducer.IntentStatusClaimed
		intent.AttemptCount = st.attempt
		intent.ClaimedAt = &claimedAt
		return intent, true, nil
	}
	return reducer.Intent{}, false, nil
}

// Ack marks an intent durably completed. Acking an item that was already
// completed once is a double-completion — the bug R-14 guards against — so it is
// counted via the sticky completedOnce flag (which a re-claim never clears) and
// left observable rather than silently ignored; the run report asserts the count
// is zero. Correct fencing means Claim never re-hands a completed item, so this
// stays 0.
func (s *durableStore) Ack(_ context.Context, intent reducer.Intent, _ reducer.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.items[intent.IntentID]
	if !ok {
		return fmt.Errorf("crashreplay: ack for unknown intent %q", intent.IntentID)
	}
	if st.completedOnce {
		s.doubleAcks++
		st.phase = phaseCompleted
		return nil
	}
	st.completedOnce = true
	st.phase = phaseCompleted
	s.acks++
	return nil
}

// Fail releases the lease on a non-completed intent so it returns to the
// claimable pool. Crash recovery does not exercise this path (a crash never acks
// or fails — it just drops the worker), but the work sink contract requires it.
func (s *durableStore) Fail(_ context.Context, intent reducer.Intent, _ error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.items[intent.IntentID]
	if !ok {
		return fmt.Errorf("crashreplay: fail for unknown intent %q", intent.IntentID)
	}
	if st.phase != phaseCompleted {
		st.phase = phasePending
	}
	return nil
}

// drained reports whether every work item is durably completed.
func (s *durableStore) drained() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.order {
		if s.items[id].phase != phaseCompleted {
			return false
		}
	}
	return true
}

// total returns the number of distinct work items in the store.
func (s *durableStore) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.order)
}

// ackedCount returns how many distinct items are durably completed.
func (s *durableStore) ackedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acks
}

// doubleAckCount returns how many times Ack hit an already-completed item.
func (s *durableStore) doubleAckCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doubleAcks
}

// reclaimCount returns how many times a lapsed lease was taken over — the
// crash-recovery reclaims.
func (s *durableStore) reclaimCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reclaims
}

// maxAttemptSeen returns the highest fencing token any item reached. A value
// >= 2 proves at least one item was claimed more than once (the recovery
// reclaim).
func (s *durableStore) maxAttemptSeen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxAttempt
}
