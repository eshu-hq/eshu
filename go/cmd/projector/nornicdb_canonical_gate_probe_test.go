// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
)

// probeCapableProjectorExecutor is a raw executor implementing Execute,
// ExecuteGroup, ExecuteProbe, and RunWrite -- standing in for the Bolt
// executor once the gate/timeout/instrumented/retry chain wraps it (#6852
// review F1). It records every ExecuteProbe call so a test can assert the
// probe reaches the raw executor through the REAL composed production chain
// built by projectorCanonicalExecutorForGraphBackend, not a hand-rolled fake
// chain.
type probeCapableProjectorExecutor struct {
	mu         sync.Mutex
	probeCalls []sourcecypher.Statement
	probeFound bool
}

func (e *probeCapableProjectorExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (e *probeCapableProjectorExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}

func (e *probeCapableProjectorExecutor) ExecuteProbe(_ context.Context, stmt sourcecypher.Statement) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.probeCalls = append(e.probeCalls, stmt)
	return e.probeFound, nil
}

func (e *probeCapableProjectorExecutor) RunWrite(
	context.Context,
	string,
	map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	return storagenornicdb.DrainWriteResult{}, nil
}

func (e *probeCapableProjectorExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.probeCalls)
}

func (e *probeCapableProjectorExecutor) lastOperation() sourcecypher.Operation {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.probeCalls[len(e.probeCalls)-1].Operation
}

func bareLabelProjectorDrainRetract() sourcecypher.Statement {
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

// TestProjectorCanonicalExecutorComposedInnerStillSatisfiesProbeExecutor is
// the projector half of the #6852 review F1 regression: WrapExecutorWithGate
// (and any future wrapper that forgets to forward ProbeExecutor) could
// silently make executeDrainLoop take mode=probe_unsupported and drain
// unconditionally -- safe, but silently deleting the entire #6822 win with no
// failing test. This drives the probe through the REAL production
// composition built by projectorCanonicalExecutorForGraphBackend (the exact
// function the projector's wiring calls), not a hand-rolled fake chain: the
// gate, TimeoutExecutor, InstrumentedExecutor, and RetryingExecutor must all
// keep forwarding ExecuteProbe down to the raw executor, carrying
// sourcecypher.OperationCanonicalProbe.
func TestProjectorCanonicalExecutorComposedInnerStillSatisfiesProbeExecutor(t *testing.T) {
	getenv := func(name string) string {
		if name == graphbackpressure.CanonicalMaxInFlightEnv {
			return "8"
		}
		return ""
	}
	raw := &probeCapableProjectorExecutor{probeFound: true}
	executor := projectorCanonicalExecutorForGraphBackend(
		raw,
		runtimecfg.GraphBackendNornicDB,
		projectorNornicDBConfigForTest(t, getenv),
		getenv,
		nil,
		nil,
		nil, // no capture session: passthrough
	)
	pge, ok := executor.(storagenornicdb.PhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicdb.PhaseGroupExecutor", executor)
	}
	if _, ok := pge.Inner.(sourcecypher.ProbeExecutor); !ok {
		t.Fatalf("composed Inner = %T does not satisfy sourcecypher.ProbeExecutor (gate/timeout/instrumented/retry chain dropped the capability)", pge.Inner)
	}

	if err := pge.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{bareLabelProjectorDrainRetract()}); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	if got := raw.callCount(); got != 1 {
		t.Fatalf("raw.ExecuteProbe calls = %d, want 1 (the composed chain must forward the probe to the raw executor)", got)
	}
	if got, want := raw.lastOperation(), sourcecypher.OperationCanonicalProbe; got != want {
		t.Fatalf("probe statement Operation = %q, want %q", got, want)
	}
}
