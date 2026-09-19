// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"testing"
)

// routingDrainReader records which method (RunProbe vs RunWrite) each call
// used, so executeDrainLoop's routing can be asserted directly rather than
// inferred from cypher text.
type routingDrainReader struct {
	probeCalls int
	writeCalls int
	probeID    string
	drained    []int64
	drainIdx   int
}

// RunProbe answers the bounded existence probe: it returns one matched
// element ID when probeID is set, or no rows otherwise.
func (r *routingDrainReader) RunProbe(context.Context, string, map[string]any) (DrainWriteResult, error) {
	r.probeCalls++
	if r.probeID == "" {
		return DrainWriteResult{}, nil
	}
	return DrainWriteResult{Rows: []map[string]any{{"__id": r.probeID}}}, nil
}

// RunWrite answers one drain iteration from the scripted drained sequence.
func (r *routingDrainReader) RunWrite(context.Context, string, map[string]any) (DrainWriteResult, error) {
	r.writeCalls++
	var drained int64
	if r.drainIdx < len(r.drained) {
		drained = r.drained[r.drainIdx]
	}
	r.drainIdx++
	return DrainWriteResult{Rows: []map[string]any{{"__drained": drained}}}, nil
}

// TestExecuteDrainLoopRoutesProbeAndDrainToDistinctMethods is the #6822
// regression proving the drain loop calls RunProbe for the bounded existence
// probe and RunWrite for the drain, instead of overloading RunWrite for both
// — which mislabels the probe as a drain write for command-owned gate and
// timeout wrappers.
func TestExecuteDrainLoopRoutesProbeAndDrainToDistinctMethods(t *testing.T) {
	t.Parallel()

	reader := &routingDrainReader{probeID: "4:a:1", drained: []int64{1, 0}}
	executor := PhaseGroupExecutor{DrainReader: reader}

	if err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 1, 1, "retract"); err != nil {
		t.Fatalf("executeDrainLoop() error = %v, want nil", err)
	}
	if reader.probeCalls != 1 {
		t.Fatalf("RunProbe calls = %d, want 1 (the bounded existence probe)", reader.probeCalls)
	}
	if reader.writeCalls != 2 {
		t.Fatalf("RunWrite calls = %d, want 2 (drain, drain-zero); the probe must not count as a RunWrite call", reader.writeCalls)
	}
}
