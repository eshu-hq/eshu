// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// idleWakeSource hands out queued work items and answers empty otherwise, with no
// waiting of its own: every Claim is one poll.
type idleWakeSource struct {
	mu    sync.Mutex
	items []ScopeGenerationWork
}

func (s *idleWakeSource) enqueue(items ...ScopeGenerationWork) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, items...)
}

func (s *idleWakeSource) Claim(context.Context) (ScopeGenerationWork, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return ScopeGenerationWork{}, false, nil
	}
	work := s.items[0]
	s.items = s.items[1:]
	return work, true, nil
}

// holdingRunner holds every projection until release closes and records how many
// ran at once, so a test can count the distinct workers that claimed.
type holdingRunner struct {
	release chan struct{}
	running atomic.Int32
	peak    atomic.Int32
}

func (r *holdingRunner) Project(ctx context.Context, _ scope.IngestionScope, _ scope.ScopeGeneration, _ []facts.Envelope) (runtime.Result, error) {
	now := r.running.Add(1)
	for {
		peak := r.peak.Load()
		if now <= peak || r.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	defer r.running.Add(-1)
	select {
	case <-r.release:
	case <-ctx.Done():
	}
	return runtime.Result{}, nil
}

func idleWakeWork(n int) ScopeGenerationWork {
	id := fmt.Sprintf("scope-%d", n)
	return ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID: id, SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: id,
		},
		Generation: scope.ScopeGeneration{
			ScopeID: id, GenerationID: "gen-" + id, Status: scope.GenerationStatusPending,
			TriggerKind: scope.TriggerKindSnapshot,
		},
	}
}

// TestIdleWorkersWakeWhenASiblingClaimsAfterAnIdleSpell proves the idle backoff
// does not leave a burst to one awake worker (#11186). Four workers sit in the idle
// backoff; one idle timer fires and finds eight queued items. That worker claims after
// an idle spell, so it wakes its sleeping siblings, and at least three distinct workers
// must be projecting at once without any further timer firing. Before the wake the
// siblings slept until their own timers (up to the 5 s cap) and the peak was one.
func TestIdleWorkersWakeWhenASiblingClaimsAfterAnIdleSpell(t *testing.T) {
	t.Parallel()

	source := &idleWakeSource{}
	runner := &holdingRunner{release: make(chan struct{})}
	timerFires := make(chan struct{})
	var sleeping atomic.Int32
	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		FactStore:          &stubFactStore{facts: []facts.Envelope{{FactID: "f", FactKind: "source_node"}}},
		Runner:             runner,
		WorkSink:           &stubProjectorWorkSink{},
		DeltaBaselineFence: &fakeDeltaBaselineFence{},
		WriteMarker:        &fakeWriteMarker{},
		Workers:            4,
		// The fake idle sleep ends only when its context ends (a wake or shutdown) or one
		// timer is fired by the test: it stands for the real backoff timer.
		Wait: func(ctx context.Context, _ time.Duration) error {
			sleeping.Add(1)
			defer sleeping.Add(-1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timerFires:
				return nil
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()

	waitFor(t, "all four workers in the idle backoff", func() bool { return sleeping.Load() == 4 })
	for i := 0; i < 8; i++ {
		source.enqueue(idleWakeWork(i))
	}
	timerFires <- struct{}{} // exactly one worker's own timer fires

	waitFor(t, "three or more workers projecting after one timer fired", func() bool { return runner.peak.Load() >= 3 })
	close(runner.release)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

// TestIdleWorkerWakeIsDebounced pins that a burst of claims after idle spells wakes the
// siblings once per poll interval, not once per claim.
func TestIdleWorkerWakeIsDebounced(t *testing.T) {
	t.Parallel()

	wake := newIdleWake(time.Hour)
	first := wake.channel()
	if !wake.broadcast() {
		t.Fatal("first broadcast was suppressed, want it sent")
	}
	select {
	case <-first:
	default:
		t.Fatal("a waiter holding the channel from before the broadcast was not woken")
	}
	if wake.broadcast() {
		t.Fatal("second broadcast inside the debounce window was sent, want it suppressed")
	}
	if wake.channel() == first {
		t.Fatal("the channel was not replaced after a broadcast")
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
