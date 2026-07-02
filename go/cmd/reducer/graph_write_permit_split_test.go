// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// slowThenSignalExecutor blocks the first call until release is closed and
// signals entered once it starts blocking, so a test can pin a class's only
// permit and observe exactly when a concurrent caller on another class
// proceeds.
type slowThenSignalExecutor struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (e *slowThenSignalExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	e.once.Do(func() { close(e.entered) })
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestGraphWriteGateSplitEliminatesHeadOfLineBlocking is the structural
// regression for issue #4448. Before the split (#3652), canonicalGate and
// semanticGate were the SAME shared *cypher.BackpressureGate, so a slow write
// on one class held the only permit and starved the other class even though
// the other class's own workload never saturated its logical pool
// (head-of-line blocking). This test proves the split removes that coupling:
// with each gate bounded to exactly 1 permit, a semantic write that blocks
// forever must NOT prevent a concurrent canonical write from completing
// promptly, and vice versa.
func TestGraphWriteGateSplitEliminatesHeadOfLineBlocking(t *testing.T) {
	t.Parallel()

	t.Run("slow semantic write does not starve canonical writes", func(t *testing.T) {
		t.Parallel()
		testHeadOfLineBlockingEliminated(t, semanticClassFirst)
	})

	t.Run("slow canonical write does not starve semantic writes", func(t *testing.T) {
		t.Parallel()
		testHeadOfLineBlockingEliminated(t, canonicalClassFirst)
	})
}

// classOrder selects which class's gate is saturated by the slow holder in
// testHeadOfLineBlockingEliminated, so the same helper proves the bound is
// symmetric in both directions.
type classOrder int

const (
	semanticClassFirst classOrder = iota
	canonicalClassFirst
)

// testHeadOfLineBlockingEliminated saturates one class's single-permit gate
// with a write that blocks forever, then proves a write on the OTHER class
// completes within a short deadline instead of queuing behind the stuck
// permit. A pre-#4448 shared pool would fail this test because the "other
// class" write would draw from the same gate the holder pinned and therefore
// never acquire a permit within the deadline.
func testHeadOfLineBlockingEliminated(t *testing.T, order classOrder) {
	t.Helper()

	const maxInFlight = 1

	gate := newReducerGraphWriteGate(func(name string) string {
		switch name {
		case graphbackpressure.CanonicalMaxInFlightEnv, graphbackpressure.SemanticMaxInFlightEnv:
			return strconv.Itoa(maxInFlight)
		default:
			return ""
		}
	}, nil)

	holder := &slowThenSignalExecutor{release: make(chan struct{}), entered: make(chan struct{})}
	other := &countingProbeExecutor{}

	var holderBound, otherBound sourcecypher.Executor
	switch order {
	case semanticClassFirst:
		holderBound = gate.boundSemanticEntityExecutorForTest(holder)
		otherBound = gate.boundExecutor(other)
	case canonicalClassFirst:
		holderBound = gate.boundExecutor(holder)
		otherBound = gate.boundSemanticEntityExecutorForTest(other)
	}

	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_ = holderBound.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
	}()
	select {
	case <-holder.entered:
		// holder now pins the single permit on its class's gate.
	case <-time.After(2 * time.Second):
		t.Fatal("holder never entered, cannot prove starvation is absent")
	}

	// The write on the OTHER class must complete promptly. Under the pre-#4448
	// shared pool this would block for the full holder lifetime because both
	// classes drew from one gate; with independent gates it must return well
	// within this generous deadline.
	otherDone := make(chan error, 1)
	go func() {
		otherDone <- otherBound.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
	}()

	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatalf("other-class write error = %v, want nil", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("other-class write blocked behind the other class's stuck permit (head-of-line blocking not eliminated)")
	}

	if got := other.calls(); got != 1 {
		t.Fatalf("other-class write calls = %d, want 1", got)
	}

	// Release the holder so its goroutine does not leak past the test.
	close(holder.release)
	<-holderDone
}

// boundSemanticEntityExecutorForTest exercises the semantic gate directly with
// a caller-supplied executor, bypassing semanticEntityExecutorForGraphBackend's
// backend-specific timeout/retry composition so the test isolates the permit
// pool boundary from unrelated adapter behavior.
func (g reducerGraphWriteGate) boundSemanticEntityExecutorForTest(inner sourcecypher.Executor) sourcecypher.Executor {
	return graphbackpressure.WrapExecutorWithGate(inner, g.semanticGate)
}

// countingProbeExecutor records how many times Execute was called, for tests
// that only need to prove a write completed rather than measure concurrency.
type countingProbeExecutor struct {
	mu sync.Mutex
	n  int
}

func (e *countingProbeExecutor) Execute(context.Context, sourcecypher.Statement) error {
	e.mu.Lock()
	e.n++
	e.mu.Unlock()
	return nil
}

func (e *countingProbeExecutor) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.n
}
