// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func collectorGitSnapshotterForEnv(t *testing.T, env map[string]string) gitrepo.NativeRepositorySnapshotter {
	t.Helper()

	service, err := buildCollectorService(
		postgres.SQLDB{},
		func(key string) string { return env[key] },
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v, want nil", err)
	}
	return service.Source.(*gitrepo.GitSource).Snapshotter.(gitrepo.NativeRepositorySnapshotter)
}

// TestBuildCollectorServiceHonorsEmitDataflowGate covers the one binary that
// already read ESHU_EMIT_DATAFLOW — and had no test saying so.
//
// Issue #5692 was that the other two parsing processes silently ignored the
// gate. The reason nobody noticed is that no process had a test asserting it
// honoured the variable, including this one. With bootstrap-index and the
// ingester now covered, all three construction sites of
// NativeRepositorySnapshotter are pinned, so a fourth one added later that
// forgets the gate is a visible gap rather than another silent empty answer.
func TestBuildCollectorServiceHonorsEmitDataflowGate(t *testing.T) {
	t.Parallel()

	if snapshotter := collectorGitSnapshotterForEnv(t, map[string]string{"ESHU_EMIT_DATAFLOW": "true"}); !snapshotter.EmitDataflow {
		t.Fatal("EmitDataflow = false with ESHU_EMIT_DATAFLOW=true, want true")
	}
}

// TestBuildCollectorServiceLeavesDataflowOffByDefault keeps this binary's
// default aligned with the other two.
func TestBuildCollectorServiceLeavesDataflowOffByDefault(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"unset": {},
		"empty": {"ESHU_EMIT_DATAFLOW": ""},
		"false": {"ESHU_EMIT_DATAFLOW": "false"},
		"junk":  {"ESHU_EMIT_DATAFLOW": "maybe"},
	} {
		if snapshotter := collectorGitSnapshotterForEnv(t, env); snapshotter.EmitDataflow {
			t.Errorf("%s: EmitDataflow = true, want false", name)
		}
	}
}
