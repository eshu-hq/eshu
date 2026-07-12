// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// barrierCommitter deterministically coordinates lane-contract tests: commits
// for scopes in holdScopes block until release is closed; failScope fails
// AFTER failGate is closed. It records completed commits and whether any
// commit context was canceled mid-commit.
type barrierCommitter struct {
	fakeCommitter

	mu        sync.Mutex
	completed []string

	inFlight    atomic.Int64
	maxInFlight atomic.Int64
	// perKeyInFlight tracks concurrent commits per admission key so conflict
	// tests can assert serialization.
	perKeyMu       sync.Mutex
	perKeyInFlight map[string]int
	perKeyOverlap  bool

	holdScopes map[string]struct{}
	release    chan struct{}
	// heldArrived is closed by the test once all holdScopes are in flight;
	// committers signal arrival via arrivals.
	arrivals chan string

	failScope string
	failGate  chan struct{}

	// commitDelay widens the in-flight overlap window; applied INSIDE the
	// in-flight accounting so concurrency assertions observe it.
	commitDelay time.Duration

	canceledMidCommit atomic.Bool
}

func (c *barrierCommitter) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	if factStream != nil {
		for range factStream {
		}
	}
	cur := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	for {
		hw := c.maxInFlight.Load()
		if cur <= hw || c.maxInFlight.CompareAndSwap(hw, cur) {
			break
		}
	}

	key := scopeValue.ScopeID
	c.perKeyMu.Lock()
	if c.perKeyInFlight == nil {
		c.perKeyInFlight = map[string]int{}
	}
	c.perKeyInFlight[key]++
	if c.perKeyInFlight[key] > 1 {
		c.perKeyOverlap = true
	}
	c.perKeyMu.Unlock()
	defer func() {
		c.perKeyMu.Lock()
		c.perKeyInFlight[key]--
		c.perKeyMu.Unlock()
	}()

	if c.arrivals != nil {
		select {
		case c.arrivals <- scopeValue.ScopeID:
		default:
		}
	}

	if c.commitDelay > 0 {
		time.Sleep(c.commitDelay)
	}

	if c.failScope == scopeValue.ScopeID {
		if c.failGate != nil {
			<-c.failGate
		}
		return fmt.Errorf("boom: injected commit failure for %s", scopeValue.ScopeID)
	}

	if _, held := c.holdScopes[scopeValue.ScopeID]; held && c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
			c.canceledMidCommit.Store(true)
			return ctx.Err()
		}
	}

	c.mu.Lock()
	c.completed = append(c.completed, scopeValue.ScopeID)
	c.mu.Unlock()
	return nil
}

// TestDrainCollectorLaneFailureLetsAdmittedSiblingsFinish pins P1 finding 1
// from the #5135 review: a lane commit failure must stop NEW admission but
// let already-admitted sibling commits finish atomically under the parent
// context — never cancel them mid-transaction.
func TestDrainCollectorLaneFailureLetsAdmittedSiblingsFinish(t *testing.T) {
	t.Parallel()

	const lanes = 4
	const total = 24
	committer := &barrierCommitter{
		holdScopes: map[string]struct{}{
			"scope-001": {}, "scope-002": {}, "scope-003": {},
		},
		release:   make(chan struct{}),
		arrivals:  make(chan string, total),
		failScope: "scope-000",
		failGate:  make(chan struct{}),
	}
	source := &fakeSource{generations: laneTestGenerations(total)}

	done := make(chan error, 1)
	go func() {
		done <- drainCollector(context.Background(), source, committer, nil, nil, nil, lanes)
	}()

	// Wait until the failing scope AND all three held siblings are in flight.
	arrived := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for len(arrived) < 4 {
		select {
		case id := <-committer.arrivals:
			arrived[id] = true
		case <-deadline:
			t.Fatalf("timed out waiting for 4 in-flight commits, have %v", arrived)
		}
	}

	// Trigger the failure while siblings are mid-commit, give the dispatcher
	// a moment to observe it, then release the siblings.
	close(committer.failGate)
	time.Sleep(50 * time.Millisecond)
	close(committer.release)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("drainCollector() error = nil, want injected commit failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drainCollector did not return after failure + sibling release")
	}

	if committer.canceledMidCommit.Load() {
		t.Fatal("an admitted sibling commit observed context cancellation mid-commit; admitted work must finish under the parent context")
	}
	committer.mu.Lock()
	completed := append([]string(nil), committer.completed...)
	committer.mu.Unlock()
	for _, want := range []string{"scope-001", "scope-002", "scope-003"} {
		found := false
		for _, got := range completed {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("admitted sibling %s did not complete its commit; completed=%v", want, completed)
		}
	}
	// No NEW admissions after the failure: only the 4 in-flight scopes ever
	// reached the committer.
	if len(completed) > 3 {
		t.Fatalf("completed %d commits, want exactly the 3 admitted siblings (no post-failure admission): %v", len(completed), completed)
	}
}

// producerSource mirrors production fact emission: each generation's Facts
// channel is fed by a producer goroutine whose blocking sends select on the
// context of the FIRST Next call — exactly GitSource's snapshot workers,
// which derive their worker context from the stream-starting Next context.
// Producers therefore unblock either by being drained to exhaustion or when
// admission stops, and never otherwise.
type producerSource struct {
	gens      []collector.CollectedGeneration
	factCount int
	producers *sync.WaitGroup
	index     int
	streamCtx context.Context
}

func (s *producerSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if s.streamCtx == nil {
		s.streamCtx = ctx
	}
	if err := ctx.Err(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.index >= len(s.gens) {
		return collector.CollectedGeneration{}, false, nil
	}
	gen := s.gens[s.index]
	i := s.index
	s.index++

	ch := make(chan facts.Envelope)
	streamCtx := s.streamCtx
	s.producers.Add(1)
	go func() {
		defer s.producers.Done()
		defer close(ch)
		for f := 0; f < s.factCount; f++ {
			select {
			case ch <- facts.Envelope{
				FactID:       fmt.Sprintf("fact-%03d-%03d", i, f),
				ScopeID:      gen.Scope.ScopeID,
				GenerationID: gen.Generation.GenerationID,
				FactKind:     "code_entity",
			}:
			case <-streamCtx.Done():
				return
			}
		}
	}()
	gen.Facts = ch
	return gen, true, nil
}

// TestDrainCollectorDrainsUndispatchedGenerationsOnFailure pins P1 finding 2
// from the #5135 review: after a lane failure stops admission, any
// generation already received from the source but never dispatched must have
// its fact stream drained to exhaustion so blocking-send producer goroutines
// are not stranded.
func TestDrainCollectorDrainsUndispatchedGenerationsOnFailure(t *testing.T) {
	t.Parallel()

	var producers sync.WaitGroup
	source := &producerSource{
		gens:      laneTestGenerations(8),
		factCount: 50,
		producers: &producers,
	}
	committer := &laneRecordingCommitter{
		commitDelay: 5 * time.Millisecond,
		failScope:   "scope-000",
	}

	if err := drainCollector(context.Background(), source, committer, nil, nil, nil, 2); err == nil {
		t.Fatal("drainCollector() error = nil, want injected commit failure")
	}

	producersExited := make(chan struct{})
	go func() {
		producers.Wait()
		close(producersExited)
	}()
	select {
	case <-producersExited:
	case <-time.After(5 * time.Second):
		t.Fatal("fact-stream producer goroutines stranded after lane failure: undispatched generations were not drained to exhaustion")
	}
}

// TestDrainCollectorSerializesConflictingScopeKeys pins P1 finding 3 from
// the #5135 review: generations sharing a ScopeID (or PartitionKey) must
// never commit concurrently, while independent scopes keep full lane
// concurrency.
func TestDrainCollectorSerializesConflictingScopeKeys(t *testing.T) {
	t.Parallel()

	// 3 generations for the SAME scope interleaved with unique scopes.
	gens := make([]collector.CollectedGeneration, 0, 12)
	for i := 0; i < 12; i++ {
		gen := laneTestGenerations(i + 1)[i]
		if i%4 == 0 {
			gen.Scope.ScopeID = "scope-shared"
			gen.Scope.PartitionKey = "repo-shared"
			gen.Generation.GenerationID = fmt.Sprintf("gen-shared-%d", i)
			gen.Generation.ScopeID = "scope-shared"
		}
		gens = append(gens, gen)
	}
	source := &fakeSource{generations: gens}
	committer := &barrierCommitter{commitDelay: 5 * time.Millisecond}
	if err := drainCollector(context.Background(), source, committer, nil, nil, nil, 4); err != nil {
		t.Fatalf("drainCollector() error = %v, want nil", err)
	}

	committer.perKeyMu.Lock()
	overlap := committer.perKeyOverlap
	committer.perKeyMu.Unlock()
	if overlap {
		t.Fatal("two generations sharing a scope key committed concurrently; keyed admission must serialize them")
	}
	committer.mu.Lock()
	total := len(committer.completed)
	committer.mu.Unlock()
	if total != 12 {
		t.Fatalf("completed %d commits, want 12 (keyed admission must not drop work)", total)
	}
	if hw := committer.maxInFlight.Load(); hw < 2 {
		t.Fatalf("max in-flight = %d, want >= 2 (independent scopes must keep concurrency)", hw)
	}
}

// TestEffectiveCommitLanes pins P1 finding 4 from the #5135 review: requested
// lanes are bounded by the measured 4-lane plateau AND by shared Postgres
// pool headroom — max(2, projectionWorkers+1) connections stay reserved —
// without ever dropping below one lane.
func TestEffectiveCommitLanes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		requested, maxConns, projWorkers, want int
	}{
		{4, 30, 8, 4}, // accepted profile shape: plenty of headroom
		{4, 96, 8, 4}, // reference profile pg96
		{8, 30, 8, 4}, // plateau bound: >4 never measured a win
		{1, 30, 8, 1}, // explicit serial
		{4, 6, 4, 1},  // budget 6 - max(2,5) = 1
		{4, 10, 4, 4}, // budget 10 - 5 = 5 -> plateau 4
		{4, 3, 8, 1},  // budget negative -> floor 1
		{4, 30, 0, 4}, // no projector: reserve max(2,1)=2
		{0, 30, 8, 1}, // floor
	}
	for _, tc := range cases {
		if got := effectiveCommitLanes(tc.requested, tc.maxConns, tc.projWorkers); got != tc.want {
			t.Fatalf("effectiveCommitLanes(%d, %d, %d) = %d, want %d",
				tc.requested, tc.maxConns, tc.projWorkers, got, tc.want)
		}
	}
}
