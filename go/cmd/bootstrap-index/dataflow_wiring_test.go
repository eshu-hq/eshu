// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
)

func bootstrapSnapshotterForEnv(t *testing.T, env map[string]string) collector.NativeRepositorySnapshotter {
	t.Helper()

	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(key string) string { return env[key] },
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}
	return deps.source.(*collector.GitSource).Snapshotter.(collector.NativeRepositorySnapshotter)
}

// TestBuildBootstrapCollectorHonorsEmitDataflowGate is the regression guard for
// issue #5692. ESHU_EMIT_DATAFLOW was read only by cmd/collector-git, which the
// default stack does not run. bootstrap-index is what seeds a local or
// deployment index, so an operator who set the variable and re-indexed got no
// code_dataflow_function facts at all, and code_flow.reaching_def stayed empty
// with coverage.state "partial" — with nothing saying the gate was ignored.
func TestBuildBootstrapCollectorHonorsEmitDataflowGate(t *testing.T) {
	t.Parallel()

	if snapshotter := bootstrapSnapshotterForEnv(t, map[string]string{"ESHU_EMIT_DATAFLOW": "true"}); !snapshotter.EmitDataflow {
		t.Fatal("EmitDataflow = false with ESHU_EMIT_DATAFLOW=true, want true")
	}
}

// TestBuildBootstrapCollectorLeavesDataflowOffByDefault keeps the gate opt-in.
// Value-flow lowering runs per function on every parsed file, so flipping it on
// by default would change the cost of every bootstrap without an operator
// asking for it.
func TestBuildBootstrapCollectorLeavesDataflowOffByDefault(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"unset": {},
		"empty": {"ESHU_EMIT_DATAFLOW": ""},
		"false": {"ESHU_EMIT_DATAFLOW": "false"},
		"junk":  {"ESHU_EMIT_DATAFLOW": "maybe"},
	} {
		if snapshotter := bootstrapSnapshotterForEnv(t, env); snapshotter.EmitDataflow {
			t.Errorf("%s: EmitDataflow = true, want false", name)
		}
	}
}
