// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// recordingBackpressureObserver records every operation label an Acquire call
// reports after actually contending for a permit (the fast, uncontended path
// never calls the observer), so a test can assert which label a wrapper used
// without reaching into gate internals.
type recordingBackpressureObserver struct {
	mu  sync.Mutex
	ops []string
}

func (o *recordingBackpressureObserver) ObserveBackpressureWait(_ context.Context, operation string, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ops = append(o.ops, operation)
}

func (o *recordingBackpressureObserver) last() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.ops) == 0 {
		return ""
	}
	return o.ops[len(o.ops)-1]
}

// TestGatedDrainReaderLabelsDrainWrite is the #6822 regression, narrowed by
// #6852: the bounded existence probe that used to share this wrapper now
// dispatches through PhaseGroupExecutor.Inner as a sourcecypher.ProbeExecutor
// instead (see TestExecuteDrainLoopRoutesProbeThroughInnerWithCanonicalProbeOperation
// in internal/storage/nornicdb), so gatedDrainReader only needs to label its
// remaining DETACH DELETE drain write.
func TestGatedDrainReaderLabelsDrainWrite(t *testing.T) {
	t.Parallel()

	observer := &recordingBackpressureObserver{}
	gate := sourcecypher.NewBackpressureGate(1, observer)
	reader := gatedDrainReader{inner: drainCapableExecutor{}, gate: gate}

	// Occupy the sole permit ourselves so the wrapper's own Acquire call must
	// contend (and therefore reports its label to the observer).
	release, err := gate.Acquire(context.Background(), "hold")
	if err != nil {
		t.Fatalf("Acquire() error = %v, want nil", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := reader.RunWrite(context.Background(), "DETACH DELETE n", nil)
		done <- err
	}()
	// Give the contending goroutine time to reach its own Acquire call while
	// the permit is still held (mirrors the sleep-then-release pattern
	// already used for gate contention in
	// cmd/projector/runtime_wiring_concurrency_test.go).
	time.Sleep(50 * time.Millisecond)
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWrite() error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWrite did not finish after releasing the held permit")
	}
	if got, want := observer.last(), "canonical_retract_drain"; got != want {
		t.Fatalf("acquire label = %q, want %q", got, want)
	}
}
