// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestBootstrapCanonicalExecutorGroupedWritesStillUsesPerPhase is the #4027
// regression: with ESHU_NORNICDB_CANONICAL_GROUPED_WRITES enabled, the NornicDB
// bootstrap canonical executor must STILL be the per-dependency-phase executor,
// not the bare grouped GroupExecutor. Whole-materialization atomic writes drop
// every file/entity nested under a directory on NornicDB (an UNWIND-driven MATCH
// does not see a node MERGE'd earlier in the same transaction), so it must never
// expose GroupExecutor regardless of the toggle.
func TestBootstrapCanonicalExecutorGroupedWritesStillUsesPerPhase(t *testing.T) {
	t.Parallel()

	raw := &recordingBootstrapGroupExecutor{}
	executor, err := bootstrapCanonicalExecutorForGraphBackend(
		raw,
		runtime.GraphBackendNornicDB,
		func(key string) string {
			if key == nornicDBCanonicalGroupedWritesEnv {
				return "true"
			}
			return ""
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("bootstrapCanonicalExecutorForGraphBackend() error = %v, want nil", err)
	}
	if _, ok := executor.(sourcecypher.GroupExecutor); ok {
		t.Fatal("grouped-writes NornicDB bootstrap executor exposes GroupExecutor (whole-materialization atomic drops nested files, #4027); want per-phase executor only")
	}
	if _, ok := executor.(sourcecypher.PhaseGroupExecutor); !ok {
		t.Fatal("grouped-writes NornicDB bootstrap executor does not implement PhaseGroupExecutor (#4027)")
	}
}
