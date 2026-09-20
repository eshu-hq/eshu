// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// probeCapableIngesterExecutor is a raw executor implementing Execute,
// ExecuteGroup, ExecuteProbe, and RunWrite -- standing in for the Bolt
// executor once the gate/timeout/instrumented/retry chain wraps it (#6852
// review F1). It records every ExecuteProbe call so a test can assert the
// probe reaches the raw executor through the REAL composed production chain,
// not a hand-rolled fake chain.
type probeCapableIngesterExecutor struct {
	mu         sync.Mutex
	probeCalls []sourcecypher.Statement
	probeFound bool
}

func (e *probeCapableIngesterExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (e *probeCapableIngesterExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}

func (e *probeCapableIngesterExecutor) ExecuteProbe(_ context.Context, stmt sourcecypher.Statement) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.probeCalls = append(e.probeCalls, stmt)
	return e.probeFound, nil
}

func (e *probeCapableIngesterExecutor) RunWrite(context.Context, string, map[string]any) (DrainWriteResult, error) {
	return DrainWriteResult{}, nil
}

func (e *probeCapableIngesterExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.probeCalls)
}

func (e *probeCapableIngesterExecutor) lastOperation() sourcecypher.Operation {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.probeCalls[len(e.probeCalls)-1].Operation
}

func bareLabelIngesterDrainRetract() sourcecypher.Statement {
	return sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher: "MATCH (n:Function)\n" +
			"WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id\n" +
			"DETACH DELETE n",
		Parameters: map[string]any{"repo_id": "repo-1", "generation_id": "gen-2"},
		Drain:      true,
		DrainVar:   "n",
	}
}

// TestCanonicalExecutorComposedInnerStillSatisfiesProbeExecutor is the #6852
// review F1 regression. sourcecypher.WrapExecutorWithGate's ExecuteOnly
// branch (and any future wrapper that forgets to forward ProbeExecutor)
// would silently make executeDrainLoop take mode=probe_unsupported and drain
// unconditionally -- safe, but silently deleting the entire #6822 win with no
// failing test. This drives the probe through the REAL production
// composition built by canonicalExecutorForGraphBackend (the exact function
// the ingester's wiring calls), not a hand-rolled fake chain: gate,
// TimeoutExecutor, InstrumentedExecutor, and RetryingExecutor must all keep
// forwarding ExecuteProbe down to the raw executor, carrying
// sourcecypher.OperationCanonicalProbe.
func TestCanonicalExecutorComposedInnerStillSatisfiesProbeExecutor(t *testing.T) {
	getenv := func(name string) string {
		if name == graphbackpressure.MaxInFlightEnv {
			return "8"
		}
		return ""
	}
	raw := &probeCapableIngesterExecutor{probeFound: true}
	executor := newTestNornicDBCanonicalExecutorWithRaw(raw, newIngesterCanonicalGate(getenv, nil))
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if _, ok := pge.Inner.(sourcecypher.ProbeExecutor); !ok {
		t.Fatalf("composed Inner = %T does not satisfy sourcecypher.ProbeExecutor (gate/timeout/instrumented/retry chain dropped the capability)", pge.Inner)
	}

	if err := pge.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{bareLabelIngesterDrainRetract()}); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	if got := raw.callCount(); got != 1 {
		t.Fatalf("raw.ExecuteProbe calls = %d, want 1 (the composed chain must forward the probe to the raw executor)", got)
	}
	if got, want := raw.lastOperation(), sourcecypher.OperationCanonicalProbe; got != want {
		t.Fatalf("probe statement Operation = %q, want %q", got, want)
	}
}

// TestCanonicalExecutorComposedChainLabelsProbeAndDrainDistinctly proves the
// REAL composed production chain (not a hand-rolled fake) labels the bounded
// existence probe `canonical_probe` and the drain write
// `canonical_retract_drain` at the shared canonical backpressure gate,
// reusing the hold-the-only-permit contention trick the deleted #6822
// wrapper tests used (recordingBackpressureObserver, defined in
// wiring_gated_drain_reader_test.go).
func TestCanonicalExecutorComposedChainLabelsProbeAndDrainDistinctly(t *testing.T) {
	observer := &recordingBackpressureObserver{}
	gate := sourcecypher.NewBackpressureGate(1, observer)
	raw := &probeCapableIngesterExecutor{probeFound: true}
	executor := newTestNornicDBCanonicalExecutorWithRaw(raw, gate)
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	probeExecutor, ok := pge.Inner.(sourcecypher.ProbeExecutor)
	if !ok {
		t.Fatalf("composed Inner = %T does not satisfy sourcecypher.ProbeExecutor", pge.Inner)
	}
	if pge.DrainReader == nil {
		t.Fatal("NornicDB phase executor has no drain reader")
	}

	assertAcquireLabel := func(t *testing.T, call func() error, want string) {
		t.Helper()

		// Occupy the sole permit ourselves so the composed chain's own
		// Acquire call must contend (and therefore reports its label to the
		// observer); the fast, uncontended path never calls the observer.
		release, err := gate.Acquire(context.Background(), "hold")
		if err != nil {
			t.Fatalf("Acquire() error = %v, want nil", err)
		}
		done := make(chan error, 1)
		go func() { done <- call() }()
		time.Sleep(50 * time.Millisecond)
		release()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("call() error = %v, want nil", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("call did not finish after releasing the held permit")
		}
		if got := observer.last(); got != want {
			t.Fatalf("acquire label = %q, want %q", got, want)
		}
	}

	assertAcquireLabel(t, func() error {
		_, err := probeExecutor.ExecuteProbe(context.Background(), sourcecypher.Statement{
			Operation: sourcecypher.OperationCanonicalProbe,
			Cypher:    "RETURN elementId(n) LIMIT 1",
		})
		return err
	}, "canonical_probe")

	assertAcquireLabel(t, func() error {
		_, err := pge.DrainReader.RunWrite(context.Background(), "DETACH DELETE n", nil)
		return err
	}, "canonical_retract_drain")
}
