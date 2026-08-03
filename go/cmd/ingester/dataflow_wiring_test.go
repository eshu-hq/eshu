// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func ingesterSnapshotterForEnv(t *testing.T, env map[string]string) collector.NativeRepositorySnapshotter {
	t.Helper()

	service, err := buildIngesterCollectorService(
		postgres.SQLDB{},
		mapGetenv(env),
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil, // tracer
		nil, // instruments
		nil, // logger
	)
	if err != nil {
		t.Fatalf("buildIngesterCollectorService() error = %v, want nil", err)
	}
	return service.Source.(*collector.GitSource).Snapshotter.(collector.NativeRepositorySnapshotter)
}

// TestBuildIngesterCollectorServiceHonorsEmitDataflowGate is the regression
// guard for issue #5692. ESHU_EMIT_DATAFLOW was read only by cmd/collector-git,
// which the default stack does not run: the ingester built its snapshotter
// without the gate, so setting the variable and re-indexing produced no
// code_dataflow_function facts and code_flow.reaching_def stayed empty with
// coverage.state "partial", with nothing in the logs saying the gate had been
// ignored.
func TestBuildIngesterCollectorServiceHonorsEmitDataflowGate(t *testing.T) {
	t.Parallel()

	if snapshotter := ingesterSnapshotterForEnv(t, map[string]string{"ESHU_EMIT_DATAFLOW": "true"}); !snapshotter.EmitDataflow {
		t.Fatal("EmitDataflow = false with ESHU_EMIT_DATAFLOW=true, want true")
	}
}

// TestBuildIngesterCollectorServiceLeavesDataflowOffByDefault keeps the gate
// opt-in. Value-flow lowering runs per function on every parsed file, so
// flipping it on by default would change the cost of every ingest without an
// operator asking for it.
func TestBuildIngesterCollectorServiceLeavesDataflowOffByDefault(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"unset": {},
		"empty": {"ESHU_EMIT_DATAFLOW": ""},
		"false": {"ESHU_EMIT_DATAFLOW": "false"},
		"junk":  {"ESHU_EMIT_DATAFLOW": "maybe"},
	} {
		if snapshotter := ingesterSnapshotterForEnv(t, env); snapshotter.EmitDataflow {
			t.Errorf("%s: EmitDataflow = true, want false", name)
		}
	}
}
