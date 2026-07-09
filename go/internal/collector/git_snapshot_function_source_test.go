// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildFunctionSourcesReadsBucket proves the dataflow_sources bucket rows are
// read into per-source snapshots.
func TestBuildFunctionSourcesReadsBucket(t *testing.T) {
	t.Parallel()
	parsed := []map[string]any{{
		"path": "/repo/h.go",
		"dataflow_sources": []map[string]any{
			{"function_id": "repo-1\x1fpkg\x1f\x1fhandle", "param_index": 0, "kind": "http_request", "lang": "go"},
			{"function_id": "", "kind": "http_request"},
		},
	}}
	sources := buildFunctionSources(parsed)
	if len(sources) != 1 {
		t.Fatalf("want 1 source (blank id dropped), got %d: %+v", len(sources), sources)
	}
	if sources[0].FunctionID != "repo-1\x1fpkg\x1f\x1fhandle" || sources[0].Kind != "http_request" || sources[0].ParamIndex != 0 {
		t.Fatalf("source not read: %+v", sources[0])
	}
}

// TestFunctionSourceFactEmittedAndCounted proves a source snapshot is streamed as
// a code_function_source fact and counted in FactCount.
func TestFunctionSourceFactEmittedAndCounted(t *testing.T) {
	t.Parallel()
	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	base := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withSrc := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withSrc.FunctionSources = []FunctionSourceSnapshot{
		{FunctionID: "repo-1\x1fpkg\x1f\x1fhandle", ParamIndex: 0, Kind: "http_request", Language: "go"},
	}
	baseFacts := drainFactChannel(buildStreamingGeneration(repoPath, repo, "run-1", observedAt, base, false).Facts)
	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, withSrc, false)
	envelopes := drainFactChannel(collected.Facts)

	if got, want := len(envelopes), collected.FactCount(); got != want {
		t.Fatalf("streamed = %d, FactCount = %d (source not counted)", got, want)
	}
	if got := len(envelopes) - len(baseFacts); got != 1 {
		t.Fatalf("source added %d facts, want 1", got)
	}
	found := false
	for _, e := range envelopes {
		if e.FactKind == facts.CodeFunctionSourceFactKind {
			found = true
		}
	}
	if !found {
		t.Fatal("no code_function_source fact emitted")
	}
}
