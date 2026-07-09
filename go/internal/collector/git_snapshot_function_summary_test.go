// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildFunctionSummariesReadsBucket proves the dataflow_summaries bucket rows
// are read into per-function snapshots carrying the FunctionID and effect lists.
func TestBuildFunctionSummariesReadsBucket(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{
		"path": "/repo/src/handler.go",
		"dataflow_summaries": []map[string]any{
			{
				"function_id":      "repo-1\x1fpkg\x1f\x1fview",
				"lang":             "go",
				"source_to_return": []any{"http_request"},
			},
			{
				"function_id":   "repo-1\x1fpkg\x1f\x1fquery",
				"lang":          "go",
				"param_to_sink": []map[string]any{{"param": 1, "sink_kind": "sql"}},
			},
		},
	}}
	entities := []content.EntityRecord{
		{EntityID: "uid-view", Path: "src/handler.go", EntityType: "Function", EntityName: "view"},
		{EntityID: "uid-query", Path: "src/handler.go", EntityType: "Function", EntityName: "query"},
	}
	summaries := buildFunctionSummaries("/repo", parsed, newFunctionUIDResolver(entities))
	if len(summaries) != 2 {
		t.Fatalf("want 2 summaries, got %d: %+v", len(summaries), summaries)
	}
	byID := map[string]FunctionSummarySnapshot{}
	for _, s := range summaries {
		byID[s.FunctionID] = s
	}
	query := byID["repo-1\x1fpkg\x1f\x1fquery"]
	if len(query.ParamToSink) != 1 || query.ParamToSink[0]["sink_kind"] != "sql" {
		t.Fatalf("query param_to_sink not read: %+v", query)
	}
	if query.GraphUID != "uid-query" {
		t.Fatalf("query graph uid not resolved: %+v", query)
	}
	if len(byID["repo-1\x1fpkg\x1f\x1fview"].SourceToReturn) != 1 {
		t.Fatalf("view source_to_return not read: %+v", byID["repo-1\x1fpkg\x1f\x1fview"])
	}
	if byID["repo-1\x1fpkg\x1f\x1fview"].GraphUID != "uid-view" {
		t.Fatalf("view graph uid not resolved: %+v", byID["repo-1\x1fpkg\x1f\x1fview"])
	}
}

// TestFunctionSummaryFactEmittedAndCounted proves a summary snapshot is streamed
// as a code_function_summary fact and counted in FactCount, so the reducer can
// persist it.
func TestFunctionSummaryFactEmittedAndCounted(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	base := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withSummaries := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withSummaries.FunctionSummaries = []FunctionSummarySnapshot{
		{FunctionID: "repo-1\x1fpkg\x1f\x1fview", Language: "go", SourceToReturn: []any{"http_request"}},
	}

	baseFacts := drainFactChannel(buildStreamingGeneration(repoPath, repo, "run-1", observedAt, base, false).Facts)
	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, withSummaries, false)
	envelopes := drainFactChannel(collected.Facts)

	if got, want := len(envelopes), collected.FactCount(); got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d (summary not counted)", got, want)
	}
	if got := len(envelopes) - len(baseFacts); got != 1 {
		t.Fatalf("function summary added %d facts, want 1", got)
	}
	found := false
	for _, e := range envelopes {
		if e.FactKind == facts.CodeFunctionSummaryFactKind {
			found = true
			if e.Payload["function_id"] != "repo-1\x1fpkg\x1f\x1fview" {
				t.Fatalf("summary fact missing function_id: %+v", e.Payload)
			}
		}
	}
	if !found {
		t.Fatalf("no code_function_summary fact emitted")
	}
}

// TestFunctionSummaryFactEnvelopeStableKey proves the fact key is the durable
// FunctionID so re-emission of a generation is idempotent.
func TestFunctionSummaryFactEnvelopeStableKey(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	s := FunctionSummarySnapshot{FunctionID: "repo-1\x1fpkg\x1f\x1fview", Language: "go"}
	a := functionSummaryFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, s)
	b := functionSummaryFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, s)
	if a.FactKind != facts.CodeFunctionSummaryFactKind {
		t.Fatalf("FactKind = %q", a.FactKind)
	}
	if a.StableFactKey != "code_function_summary:repo-1:repo-1\x1fpkg\x1f\x1fview" {
		t.Fatalf("StableFactKey = %q", a.StableFactKey)
	}
	if a.StableFactKey != b.StableFactKey || a.FactID != b.FactID {
		t.Fatalf("not stable across re-emission")
	}
}
