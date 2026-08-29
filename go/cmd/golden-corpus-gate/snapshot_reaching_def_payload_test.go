// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// reachingDefDefUsePath is the response path that carries the reaching-definition
// answer itself. Every other field dispatch_reaching_def returns for a
// definition -- the function name, its language, the fact label, the evidence
// handle -- is emitted for a function whether or not a single def->use edge
// survived, because codeFlowReachingPayloads builds one row per matched
// function and hangs the payload off it. A shape that stops at those fields
// therefore passes on an empty payload and cannot fail for the reason it
// exists (eshu-hq/eshu#6090).
const reachingDefDefUsePath = "definitions[].def_use[]"

// The reaching-def shape is pinned to the same fixture function as the three
// sibling dataflow shapes (dispatch_cfg_summary, dispatch_pdg_summary,
// dispatch_taint_path), which already pin def_use_count = 4 for it. Before
// #6090 it was pinned to orders-api's main(), whose body is a single blank
// assignment: the parser measures zero def->use edges there, so there was no
// payload for the shape to assert even in principle.
const (
	goldenDataflowRepoID   = "repository:r_8477a002"
	goldenDataflowSymbol   = "GoldenDataflowHandler"
	goldenDataflowFilePath = "dataflow_proof.go"
)

// goldenReachingDefRows are the def->use edges the Go parser measures for
// GoldenDataflowHandler in
// tests/fixtures/ecosystems/go_comprehensive/dataflow_proof.go, in the
// deterministic order emitDefUses sorts them into. The same four rows are
// pinned fixture-side by TestGoldenDataflowFixtureReachesCollectorReadContracts,
// so the snapshot cannot drift away from what the fixture actually produces.
// Values are float64 because the snapshot arrives through encoding/json.
var goldenReachingDefRows = []map[string]any{
	{"binding": "r", "def_line": float64(12), "use_line": float64(13)},
	{"binding": "query", "def_line": float64(13), "use_line": float64(14)},
	{"binding": "db", "def_line": float64(12), "use_line": float64(15)},
	{"binding": "query", "def_line": float64(13), "use_line": float64(15)},
}

// TestGoldenSnapshotReachingDefPinsNonEmptyDefUsePayload proves the
// dispatch_reaching_def shape asserts the reaching-definition payload and not
// only the function metadata wrapped around it: a response whose def_use is
// null, empty, or absent must be rejected, and the measured fixture payload
// must still be accepted.
func TestGoldenSnapshotReachingDefPinsNonEmptyDefUsePayload(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.MCP["dispatch_reaching_def"]
	if !ok {
		t.Fatal("query_shapes.mcp missing dispatch_reaching_def")
	}
	if shape.MinimumResults != 1 || shape.MaximumResults != 1 || shape.ResultsField != "definitions" {
		t.Fatalf("bounds/field = [%d,%d] %q, want [1,1] definitions", shape.MinimumResults, shape.MaximumResults, shape.ResultsField)
	}
	if !containsString(shape.ResultItemRequiredFields, "def_use") {
		t.Errorf("ResultItemRequiredFields = %v, want it to require def_use", shape.ResultItemRequiredFields)
	}
	assertSnapshotPaths(t, shape.RequiredJSONPaths, []string{reachingDefDefUsePath})
	if got := shape.RequiredJSONObjectMatches[reachingDefDefUsePath]; !reflect.DeepEqual(got, goldenReachingDefRows) {
		t.Errorf("RequiredJSONObjectMatches[%q] = %#v, want %#v", reachingDefDefUsePath, got, goldenReachingDefRows)
	}
	assertSnapshotValues(t, shape.RequiredJSONValues, map[string]any{
		"query.kind":                  "reaching_def",
		"query.repo_id":               goldenDataflowRepoID,
		"query.symbol":                goldenDataflowSymbol,
		"query.file_path":             goldenDataflowFilePath,
		"bounds.count":                float64(1),
		"bounds.truncated":            false,
		"coverage.state":              "exact",
		"definitions[].function_name": goldenDataflowSymbol,
		"definitions[].relative_path": goldenDataflowFilePath,
		"definitions[].language":      "go",
		"definitions[].fact_label":    "exact_parser_fact",
		"definitions[].overflow":      false,
	})

	for _, test := range []struct {
		name   string
		mutate func(definition map[string]any)
	}{
		{name: "null_payload", mutate: func(definition map[string]any) { definition["def_use"] = nil }},
		{name: "empty_payload", mutate: func(definition map[string]any) { definition["def_use"] = []any{} }},
		{name: "absent_payload", mutate: func(definition map[string]any) { delete(definition, "def_use") }},
		// A non-empty payload whose rows are subtly wrong. This is the case the
		// three emptiness mutations cannot reach: it proves
		// RequiredJSONObjectMatches pins the def->use CONTENT, not merely that
		// some rows arrived. The row below is the first measured edge with both
		// line numbers shifted by one, which is exactly the drift a parser
		// regression would produce.
		{name: "wrong_content_payload", mutate: func(definition map[string]any) {
			definition["def_use"] = []any{
				map[string]any{"binding": "r", "def_line": float64(13), "use_line": float64(14)},
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			finding := EvaluateQueryShape("reaching-def-"+test.name, shape, reachingDefResponse(t, test.mutate))
			if finding.OK {
				t.Fatalf("reaching-def response with %s passed: %+v", test.name, finding)
			}
		})
	}
	t.Run("measured_payload", func(t *testing.T) {
		finding := EvaluateQueryShape("reaching-def-measured", shape, reachingDefResponse(t, nil))
		if !finding.OK {
			t.Fatalf("measured fixture payload failed: %+v", finding)
		}
	})
}

// reachingDefResponse renders the unwrapped MCP payload dispatch_reaching_def
// returns for the GoldenDataflowHandler fixture, applying mutate to the single
// definition row first when it is non-nil.
func reachingDefResponse(t *testing.T, mutate func(definition map[string]any)) []byte {
	t.Helper()

	definition := map[string]any{
		"repo_id":         goldenDataflowRepoID,
		"relative_path":   goldenDataflowFilePath,
		"function_name":   goldenDataflowSymbol,
		"function_uid":    "func:golden-dataflow-handler",
		"language":        "go",
		"line_number":     12,
		"evidence_handle": "fact://code_dataflow_function/func:golden-dataflow-handler",
		"generation_id":   "gen-6090",
		"fact_label":      "exact_parser_fact",
		"def_use":         goldenReachingDefRows,
		"overflow":        false,
	}
	if mutate != nil {
		mutate(definition)
	}
	body, err := json.Marshal(map[string]any{
		"query": map[string]any{
			"kind": "reaching_def", "repo_id": goldenDataflowRepoID, "language": "go",
			"symbol": goldenDataflowSymbol, "file_path": goldenDataflowFilePath, "line": 0,
		},
		"coverage": map[string]any{
			"state":    "exact",
			"reason":   "active parser dataflow facts matched the requested scope",
			"language": "go",
		},
		"bounds":      map[string]any{"limit": 1, "count": 1, "truncated": false},
		"ambiguity":   map[string]any{"ambiguous": false},
		"definitions": []any{definition},
	})
	if err != nil {
		t.Fatalf("marshal reaching-def response: %v", err)
	}
	return body
}
