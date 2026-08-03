// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// dataflowGateChainSource is a small function with a definition and a later use
// of the same variable, so value-flow lowering has a real def-use edge to find
// rather than an empty control-flow graph.
const dataflowGateChainSource = `package handlers

import "fmt"

func Handle(raw string) string {
	trimmed := raw
	if trimmed == "" {
		trimmed = "default"
	}
	out := fmt.Sprintf("%s!", trimmed)
	return out
}
`

func dataflowGateChainSnapshot(t *testing.T, emitDataflow bool) RepositorySnapshot {
	t.Helper()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "handlers.go"), dataflowGateChainSource)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}

	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{
		Now:          func() time.Time { return now },
		EmitDataflow: emitDataflow,
	}

	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository(emitDataflow=%v) error = %v", emitDataflow, err)
	}
	return snapshot
}

// TestSnapshotterEmitDataflowFieldReachesTheSnapshot closes the chain issue
// #5692 left open. The wiring tests in cmd/ingester and cmd/bootstrap-index
// prove ESHU_EMIT_DATAFLOW now reaches NativeRepositorySnapshotter.EmitDataflow;
// this proves that field actually produces the dataflow rows the
// code_dataflow_function facts are built from, by driving the real snapshotter
// over a real repository rather than asserting on the options struct.
//
// Without it the two halves could each pass while the middle stayed broken:
// a gate that arrives at a field nothing reads is exactly the failure #5692
// was, one layer down.
func TestSnapshotterEmitDataflowFieldReachesTheSnapshot(t *testing.T) {
	t.Parallel()

	on := dataflowGateChainSnapshot(t, true)
	if !on.DataflowScanned {
		t.Error("DataflowScanned = false with the gate on, want true")
	}
	if len(on.DataflowFunctions) == 0 {
		t.Fatal("DataflowFunctions is empty with the gate on: the snapshot carries no value-flow rows to build code_dataflow_function facts from")
	}

	var named int
	for _, fn := range on.DataflowFunctions {
		if fn.FunctionName == "Handle" {
			named++
		}
	}
	if named == 0 {
		t.Errorf("no dataflow row for the fixture's Handle function; got %+v", on.DataflowFunctions)
	}
}

// TestSnapshotterEmitDataflowOffKeepsSnapshotClean is the other half of the
// opt-in contract: with the gate off the snapshot must carry no value-flow
// rows at all, so an operator who never asks for it pays nothing and the
// payload shape is unchanged.
func TestSnapshotterEmitDataflowOffKeepsSnapshotClean(t *testing.T) {
	t.Parallel()

	off := dataflowGateChainSnapshot(t, false)
	if off.DataflowScanned {
		t.Error("DataflowScanned = true with the gate off, want false")
	}
	if len(off.DataflowFunctions) != 0 {
		t.Errorf("DataflowFunctions = %d rows with the gate off, want 0: %+v", len(off.DataflowFunctions), off.DataflowFunctions)
	}
}
