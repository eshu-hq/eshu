// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFirstReducerPartitionKeyUsesStableSortedKey(t *testing.T) {
	t.Parallel()

	intent := Intent{
		EntityKeys: []string{"repo:zeta", "repo:alpha", "repo:beta"},
	}

	if got, want := firstReducerPartitionKey(intent), "repo:alpha"; got != want {
		t.Fatalf("firstReducerPartitionKey() = %q, want %q", got, want)
	}
}

func TestServiceRunHeartbeatsLongRunningReducerWork(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:        "intent-heartbeat",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainSemanticEntityMaterialization,
		Cause:           "projector emitted semantic entity work",
		EntityKeys:      []string{"repo:eshu"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		Status:          IntentStatusPending,
	}

	release := make(chan struct{})
	heartbeater := &stubReducerHeartbeater{
		afterHeartbeat: func(count int) {
			if count == 2 {
				close(release)
			}
		},
	}
	executor := &blockingReducerExecutor{
		release: release,
		result: Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		},
	}
	sink := &stubReducerWorkSink{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubReducerWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: 5 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := heartbeater.calls(), 2; got < want {
		t.Fatalf("heartbeat calls = %d, want at least %d", got, want)
	}
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
}

// TestServiceRunPreHeartbeatsImmediatelyOnClaim reproduces #4447: a reducer
// claim has a startup window where the lease can expire before the first
// heartbeat because the periodic ticker inside startHeartbeat does not fire
// until a full HeartbeatInterval has elapsed. A worker that stalls (GC pause,
// slow first graph write) immediately after claim can let the lease expire
// before any heartbeat lands, causing at-least-twice execution when the lease
// is reclaimed.
//
// HeartbeatInterval is set to one hour so the periodic ticker cannot possibly
// fire during the test. The executor blocks until the heartbeater observes
// its first call. If the fix (an immediate heartbeat emitted synchronously at
// claim time, before Executor.Execute runs) is not present, the executor
// blocks forever and the test times out -- proving the startup window is
// closed only when this test passes.
func TestServiceRunPreHeartbeatsImmediatelyOnClaim(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:     "intent-pre-heartbeat",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainSemanticEntityMaterialization,
		Cause:        "projector emitted semantic entity work",
		EntityKeys:   []string{"repo:eshu"},
		EnqueuedAt:   time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	}

	firstHeartbeat := make(chan struct{})
	var closedOnce sync.Once
	heartbeater := &stubReducerHeartbeater{
		afterHeartbeat: func(count int) {
			if count == 1 {
				closedOnce.Do(func() { close(firstHeartbeat) })
			}
		},
	}
	executor := &blockingReducerExecutor{
		release: firstHeartbeat,
		result: Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		},
	}
	sink := &stubReducerWorkSink{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubReducerWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: time.Hour,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	done := make(chan error, 1)
	go func() { done <- service.Run(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not complete: executor never observed a heartbeat before the 1h ticker interval, lease-expiry startup window is not closed")
	}

	if got, want := heartbeater.calls(), 1; got < want {
		t.Fatalf("heartbeat calls = %d, want at least %d", got, want)
	}
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
}

// TestServiceRunPreHeartbeatFailureDoesNotDeadLetter reproduces a bug flagged
// in PR #4524 review (codex, service.go:459): when the new synchronous
// pre-heartbeat itself fails (a transient Postgres UPDATE error, say),
// startHeartbeat returned an already-cancelled execution context. Before this
// fix, executeWithTelemetry still called Executor.Execute with that cancelled
// context, treated the resulting context.Canceled as a normal handler
// failure, and routed it through WorkSink.Fail -- which, with the real
// ReducerQueue.Fail, can mark the intent dead_letter even though no handler
// work ever ran. A pre-start lease/heartbeat miss must leave the intent
// claimable/reclaimable (the lease is simply left unrenewed for the
// expired-lease reclaim path added in #4464), never dead-lettered.
//
// The heartbeater fails on its first (and only, in this test) call, so the
// pre-heartbeat branch in startHeartbeat is exercised deterministically. If
// the fix is not present, the never-blocking executor stub below would still
// get called -- this test asserts it is not, and that WorkSink.Fail is never
// called either.
func TestServiceRunPreHeartbeatFailureDoesNotDeadLetter(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:     "intent-pre-heartbeat-failure",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainSemanticEntityMaterialization,
		Cause:        "projector emitted semantic entity work",
		EntityKeys:   []string{"repo:eshu"},
		EnqueuedAt:   time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	}

	heartbeater := &failingReducerHeartbeater{failWith: errors.New("transient heartbeat UPDATE error")}
	executor := &neverCalledReducerExecutor{}
	sink := &stubReducerWorkSink{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubReducerWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: time.Hour,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if executor.calls() != 0 {
		t.Fatalf("Executor.Execute calls = %d, want 0: no handler work must run under a claim that lost its lease before starting", executor.calls())
	}
	if sink.failCalls != 0 {
		t.Fatalf("WorkSink.Fail calls = %d, want 0: a pre-start heartbeat miss must never be routed to Fail/dead_letter", sink.failCalls)
	}
	if sink.ackCalls != 0 {
		t.Fatalf("WorkSink.Ack calls = %d, want 0: no handler work ran, so there is nothing to ack", sink.ackCalls)
	}
}

type failingReducerHeartbeater struct {
	failWith error
}

func (f *failingReducerHeartbeater) Heartbeat(context.Context, Intent) error {
	return f.failWith
}

// neverCalledReducerExecutor asserts Executor.Execute is never invoked under
// a claim that lost its lease before starting. If it is called with a
// cancelled context -- the unpatched behavior -- it returns ctx.Err(), the
// same way a real executor observing a cancelled context would, so the
// unpatched code path reaches WorkSink.Fail exactly as codex's review
// finding describes (rather than a distinct "ack heartbeat" error path).
type neverCalledReducerExecutor struct {
	mu    sync.Mutex
	count int
}

func (n *neverCalledReducerExecutor) Execute(ctx context.Context, _ Intent) (Result, error) {
	n.mu.Lock()
	n.count++
	n.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{}, nil
}

func (n *neverCalledReducerExecutor) calls() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.count
}

type stubReducerHeartbeater struct {
	mu             sync.Mutex
	heartbeatCalls int
	afterHeartbeat func(int)
}

func (s *stubReducerHeartbeater) Heartbeat(context.Context, Intent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatCalls++
	if s.afterHeartbeat != nil {
		s.afterHeartbeat(s.heartbeatCalls)
	}
	return nil
}

func (s *stubReducerHeartbeater) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heartbeatCalls
}

type blockingReducerExecutor struct {
	release <-chan struct{}
	result  Result
}

func (b *blockingReducerExecutor) Execute(ctx context.Context, _ Intent) (Result, error) {
	select {
	case <-b.release:
		return b.result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}
