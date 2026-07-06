// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"reflect"
	"testing"
)

// TestDecodeParsedFileDataGomodState_TypedReadSet proves the gomod_state inner
// key of a parsed_file_data map decodes into the typed codegraphv1.GomodState
// struct, exposing the state and module_path fields the cross-repo-export
// reducer reads (code_call_materialization_cross_repo_export.go) while
// preserving every other producer field in the open Attributes pass-through so
// the accessor never drops go.mod/go.sum evidence.
func TestDecodeParsedFileDataGomodState_TypedReadSet(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"lang": "gomod",
		"gomod_state": map[string]any{
			"state":            "parsed",
			"module_path":      "github.com/eshu-hq/eshu",
			"go_version":       "1.23",
			"toolchain":        "go1.23.4",
			"require_count":    float64(12),
			"replaced_modules": []any{"golang.org/x/net"},
		},
	}

	state, ok, err := DecodeParsedFileDataGomodState(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataGomodState() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("DecodeParsedFileDataGomodState() ok = false, want true for a present gomod_state")
	}
	if state.State != "parsed" {
		t.Fatalf("State = %q, want %q", state.State, "parsed")
	}
	if state.ModulePath == nil || *state.ModulePath != "github.com/eshu-hq/eshu" {
		t.Fatalf("ModulePath = %v, want %q", state.ModulePath, "github.com/eshu-hq/eshu")
	}
	// Producer fields with no named struct field survive in the open passthrough.
	if state.Attributes == nil {
		t.Fatal("Attributes = nil, want the non-read producer fields captured")
	}
	if got, ok := state.Attributes["go_version"].(string); !ok || got != "1.23" {
		t.Fatalf("Attributes[go_version] = %#v, want string \"1.23\"", state.Attributes["go_version"])
	}
	// Named read-set fields must NOT leak into the passthrough.
	for _, named := range []string{"state", "module_path"} {
		if _, leaked := state.Attributes[named]; leaked {
			t.Fatalf("named field %q leaked into Attributes; it must be a typed field", named)
		}
	}
}

// TestDecodeParsedFileDataGomodState_Absent proves that a parsed_file_data map
// without a gomod_state key reports ok=false and no error, so a non-gomod file
// is a clean "no typed state" observation, not a decode failure.
func TestDecodeParsedFileDataGomodState_Absent(t *testing.T) {
	t.Parallel()

	_, ok, err := DecodeParsedFileDataGomodState(map[string]any{"lang": "go"})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataGomodState() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("DecodeParsedFileDataGomodState() ok = true, want false for an absent gomod_state")
	}
}

// TestDecodeParsedFileDataSCIPFunctionCalls_TypedEdges proves the
// function_calls_scip inner key decodes into a typed []SCIPFunctionCall
// carrying every edge field the SCIP code-call extractor reads
// (code_call_materialization_index_rows.go): caller/callee symbol, file, line,
// name, and ref_line. The int line fields survive the JSON float64 shape a
// Postgres JSONB round trip produces.
func TestDecodeParsedFileDataSCIPFunctionCalls_TypedEdges(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"function_calls_scip": []any{
			map[string]any{
				"caller_symbol": "scip go pkg `caller`().",
				"caller_file":   "/abs/caller.go",
				"caller_line":   float64(10),
				"callee_symbol": "scip go pkg `callee`().",
				"callee_file":   "/abs/callee.go",
				"callee_line":   float64(20),
				"callee_name":   "callee",
				"ref_line":      float64(11),
			},
		},
	}

	edges, err := DecodeParsedFileDataSCIPFunctionCalls(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataSCIPFunctionCalls() error = %v, want nil", err)
	}
	if len(edges) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edges))
	}
	edge := edges[0]
	if edge.CallerSymbol != "scip go pkg `caller`()." {
		t.Fatalf("CallerSymbol = %q", edge.CallerSymbol)
	}
	if edge.CallerFile != "/abs/caller.go" || edge.CalleeFile != "/abs/callee.go" {
		t.Fatalf("caller/callee file = %q / %q", edge.CallerFile, edge.CalleeFile)
	}
	if edge.CallerLine != 10 || edge.CalleeLine != 20 || edge.RefLine != 11 {
		t.Fatalf("lines = caller %d callee %d ref %d, want 10/20/11", edge.CallerLine, edge.CalleeLine, edge.RefLine)
	}
	if edge.CalleeName != "callee" {
		t.Fatalf("CalleeName = %q, want %q", edge.CalleeName, "callee")
	}
}

// TestDecodeParsedFileDataSCIPFunctionCalls_AbsentIsEmpty proves an absent
// function_calls_scip key decodes to an empty slice with no error, matching the
// reducer's mapSlice(nil) behavior (a file with no SCIP edges yields no rows).
func TestDecodeParsedFileDataSCIPFunctionCalls_AbsentIsEmpty(t *testing.T) {
	t.Parallel()

	edges, err := DecodeParsedFileDataSCIPFunctionCalls(map[string]any{"lang": "go"})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataSCIPFunctionCalls() error = %v, want nil", err)
	}
	if len(edges) != 0 {
		t.Fatalf("len(edges) = %d, want 0 for an absent key", len(edges))
	}
}

// TestDecodeParsedFileDataDockerfileStages_TypedStages proves the
// dockerfile_stages inner key decodes into a typed []DockerfileStage carrying
// the FROM-stage identity and runtime fields the dockerfile producer emits
// (go/internal/parser/dockerfile/metadata.go stageMap).
func TestDecodeParsedFileDataDockerfileStages_TypedStages(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"lang": "dockerfile",
		"dockerfile_stages": []any{
			map[string]any{
				"name":        "build",
				"line_number": float64(1),
				"stage_index": float64(0),
				"base_image":  "golang",
				"base_tag":    "1.23",
				"alias":       "build",
				"path":        "build",
				"lang":        "dockerfile",
				"workdir":     "/src",
			},
		},
	}

	stages, err := DecodeParsedFileDataDockerfileStages(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataDockerfileStages() error = %v, want nil", err)
	}
	if len(stages) != 1 {
		t.Fatalf("len(stages) = %d, want 1", len(stages))
	}
	stage := stages[0]
	if stage.Name != "build" || stage.BaseImage != "golang" || stage.BaseTag != "1.23" {
		t.Fatalf("stage identity = %q %q %q", stage.Name, stage.BaseImage, stage.BaseTag)
	}
	if stage.StageIndex != 0 || stage.LineNumber != 1 {
		t.Fatalf("stage_index/line = %d/%d, want 0/1", stage.StageIndex, stage.LineNumber)
	}
}

// TestDecodeParsedFileDataStringSliceKeys proves the two []string inner keys —
// pipeline_calls (groovy) and dead_code_file_root_kinds (javascript) — decode
// into a Go []string regardless of whether the producer emitted []string or the
// []any a JSONB round trip yields, matching the reducer's toStringSlice/
// sliceValue read behavior.
func TestDecodeParsedFileDataStringSliceKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		key     string
		decode  func(map[string]any) []string
		payload map[string]any
		want    []string
	}{
		{
			name:   "pipeline_calls_string_slice",
			decode: DecodeParsedFileDataPipelineCalls,
			payload: map[string]any{
				"pipeline_calls": []string{"pipelineDeploy", "pipelinePM2"},
			},
			want: []string{"pipelineDeploy", "pipelinePM2"},
		},
		{
			name:   "pipeline_calls_any_slice_from_jsonb",
			decode: DecodeParsedFileDataPipelineCalls,
			payload: map[string]any{
				"pipeline_calls": []any{"pipelineDeploy"},
			},
			want: []string{"pipelineDeploy"},
		},
		{
			name:   "dead_code_root_kinds",
			decode: DecodeParsedFileDataDeadCodeFileRootKinds,
			payload: map[string]any{
				"dead_code_file_root_kinds": []any{"javascript.node_package_entrypoint"},
			},
			want: []string{"javascript.node_package_entrypoint"},
		},
		{
			name:    "absent_key_is_nil",
			decode:  DecodeParsedFileDataPipelineCalls,
			payload: map[string]any{"lang": "groovy"},
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.decode(tc.payload)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s = %#v, want %#v", tc.name, got, tc.want)
			}
		})
	}
}
