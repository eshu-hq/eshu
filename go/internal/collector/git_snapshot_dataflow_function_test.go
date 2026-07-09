// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildDataflowFunctionsReadsParserBucket proves the parser's
// dataflow_functions bucket survives collection as exact parser fact input for
// CFG, reaching-def, and PDG summary read surfaces.
func TestBuildDataflowFunctionsReadsParserBucket(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{
		"path": "/repo/src/handler.go",
		"dataflow_functions": []map[string]any{{
			"function_name": "handle",
			"line_number":   3,
			"lang":          "go",
			"blocks": []map[string]any{
				{
					"id":    0,
					"succs": []int{1},
					"stmts": []map[string]any{{"id": 10, "line": 4, "defs": []string{"q"}}},
				},
				{"id": 1, "succs": []int{}, "stmts": []map[string]any{{"id": 11, "line": 5, "uses": []string{"q"}}}},
			},
			"def_uses": []map[string]any{{
				"binding":  "q",
				"def_line": 4,
				"use_line": 5,
			}},
			"overflow": map[string]any{"def_use_edges": 2, "access_paths": 1},
		}},
	}}
	entities := []content.EntityRecord{{
		EntityID:   "func-handle",
		Path:       "src/handler.go",
		EntityType: "Function",
		EntityName: "handle",
		StartLine:  3,
	}}

	functions := buildDataflowFunctions("/repo", parsed, buildEntityUIDLookup(entities))
	if len(functions) != 1 {
		t.Fatalf("want 1 dataflow function, got %d: %+v", len(functions), functions)
	}
	got := functions[0]
	if got.FunctionUID != "func-handle" {
		t.Fatalf("FunctionUID = %q, want func-handle", got.FunctionUID)
	}
	if got.RelativePath != "src/handler.go" || got.FunctionName != "handle" || got.Language != "go" {
		t.Fatalf("identity fields not mapped: %+v", got)
	}
	if len(got.CFGBlocks) != 2 || len(got.CFGEdges) != 1 {
		t.Fatalf("CFG not mapped: %+v", got)
	}
	if len(got.DefUse) != 1 || got.DefUse[0]["binding"] != "q" {
		t.Fatalf("def-use not mapped: %+v", got.DefUse)
	}
	if !got.Overflow || got.OverflowReason != "access_paths=1,def_use_edges=2" {
		t.Fatalf("overflow not mapped: %+v", got)
	}
}

// TestDataflowFunctionFactEmittedAndCounted proves raw CFG/reaching-def parser
// facts are streamed and counted so API/MCP reads do not need to re-run parsers.
func TestDataflowFunctionFactEmittedAndCounted(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 28, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	base := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withDataflow := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withDataflow.DataflowFunctions = []DataflowFunctionSnapshot{{
		FunctionUID:  "func-handle",
		RelativePath: "src/handler.go",
		FunctionName: "handle",
		Language:     "go",
		LineNumber:   3,
		CFGBlocks:    []any{map[string]any{"id": 0, "kind": "entry"}},
		DefUse:       []map[string]any{{"binding": "q", "def_line": 4, "use_line": 5}},
	}}

	baseFacts := drainFactChannel(buildStreamingGeneration(repoPath, repo, "run-1", observedAt, base, false).Facts)
	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, withDataflow, false)
	envelopes := drainFactChannel(collected.Facts)

	if got, want := len(envelopes), collected.FactCount; got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d (dataflow function not counted)", got, want)
	}
	if got := len(envelopes) - len(baseFacts); got != 1 {
		t.Fatalf("dataflow function added %d facts, want 1", got)
	}
	found := false
	for _, e := range envelopes {
		if e.FactKind != facts.CodeDataflowFunctionFactKind {
			continue
		}
		found = true
		if e.Payload["function_uid"] != "func-handle" || e.Payload["relative_path"] != "src/handler.go" {
			t.Fatalf("dataflow function payload not mapped: %+v", e.Payload)
		}
	}
	if !found {
		t.Fatalf("no %s fact emitted", facts.CodeDataflowFunctionFactKind)
	}
}
