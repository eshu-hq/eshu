// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// routingInner implements sourcecypher.Executor and sourcecypher.ProbeExecutor,
// recording the Statement each ExecuteProbe call receives so a test can assert
// its Operation without inspecting cypher text.
type routingInner struct {
	probeCalls []sourcecypher.Statement
	found      bool
}

func (r *routingInner) Execute(context.Context, sourcecypher.Statement) error { return nil }

func (r *routingInner) ExecuteProbe(_ context.Context, stmt sourcecypher.Statement) (bool, error) {
	r.probeCalls = append(r.probeCalls, stmt)
	return r.found, nil
}

// routingDrainReader records RunWrite calls only: DrainReader no longer
// carries RunProbe (#6852) -- the probe routes through PhaseGroupExecutor.Inner
// as a sourcecypher.ProbeExecutor instead.
type routingDrainReader struct {
	writeCalls int
	drained    []int64
	drainIdx   int
}

func (r *routingDrainReader) RunWrite(context.Context, string, map[string]any) (DrainWriteResult, error) {
	r.writeCalls++
	var drained int64
	if r.drainIdx < len(r.drained) {
		drained = r.drained[r.drainIdx]
	}
	r.drainIdx++
	return DrainWriteResult{Rows: []map[string]any{{"__drained": drained}}}, nil
}

// TestExecuteDrainLoopRoutesProbeThroughInnerWithCanonicalProbeOperation is
// the #6852 regression proving the drain loop dispatches the bounded
// existence probe through PhaseGroupExecutor.Inner as a
// sourcecypher.ProbeExecutor carrying sourcecypher.OperationCanonicalProbe --
// the same instrumented executor chain (backpressure, timeout,
// InstrumentedExecutor, RetryingExecutor) every other canonical write goes
// through -- and still drives the drain loop itself through
// DrainReader.RunWrite, not through a DrainReader-owned RunProbe seam (which
// bypassed that chain: no neo4j.execute_probe span, no
// Neo4jQueryDuration{operation=probe} point).
func TestExecuteDrainLoopRoutesProbeThroughInnerWithCanonicalProbeOperation(t *testing.T) {
	t.Parallel()

	inner := &routingInner{found: true}
	drain := &routingDrainReader{drained: []int64{1, 0}}
	executor := PhaseGroupExecutor{Inner: inner, DrainReader: drain}

	if err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 1, 1, "retract"); err != nil {
		t.Fatalf("executeDrainLoop() error = %v, want nil", err)
	}
	if len(inner.probeCalls) != 1 {
		t.Fatalf("Inner.ExecuteProbe calls = %d, want 1", len(inner.probeCalls))
	}
	if got, want := inner.probeCalls[0].Operation, sourcecypher.OperationCanonicalProbe; got != want {
		t.Fatalf("probe statement Operation = %q, want %q", got, want)
	}
	if drain.writeCalls != 2 {
		t.Fatalf("DrainReader.RunWrite calls = %d, want 2 (drain, drain-zero); the probe must not count as a RunWrite call", drain.writeCalls)
	}
}
