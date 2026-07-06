// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCodeTaintEvidenceReplayIsIdempotent proves that decoding and extracting
// the same code_taint_evidence fact batch twice (simulating a queue retry or
// a re-run generation replaying unchanged facts, Contract System v1 Wave 4f
// S2, issue #4754) produces byte-identical rows both times: the typed decode
// seam introduces no per-call nondeterminism (map iteration order, pointer
// identity, or similar) that would make a retried intent diverge from its
// first attempt.
func TestCodeTaintEvidenceReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "taint-a",
			FactKind: "code_taint_evidence",
			Payload: map[string]any{
				"function_uid":  "uid:fn-a",
				"repo_id":       "repo-a",
				"relative_path": "src/a.go",
				"function_name": "HandleA",
				"kind":          "sql_injection",
				"sink_kind":     "sql_exec",
				"source_kind":   "http_request",
				"source_line":   float64(3),
				"sink_line":     float64(9),
				"confidence":    0.9,
			},
		},
		{
			FactID:   "taint-b",
			FactKind: "code_taint_evidence",
			Payload: map[string]any{
				"function_uid":  "uid:fn-b",
				"repo_id":       "repo-b",
				"relative_path": "src/b.go",
				"function_name": "HandleB",
				"kind":          "command_injection",
				"sink_kind":     "os_exec",
				"source_kind":   "cli_arg",
				"source_line":   float64(4),
				"sink_line":     float64(11),
				"confidence":    0.5,
			},
		},
	}

	replay := func() []map[string]any {
		rows, _, err := ExtractCodeTaintEvidenceRowsWithQuarantine(envelopes)
		if err != nil {
			t.Fatalf("ExtractCodeTaintEvidenceRowsWithQuarantine error = %v, want nil", err)
		}
		return rows
	}

	first := replay()
	second := replay()

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("len(first)=%d len(second)=%d, want 2 rows each", len(first), len(second))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replay is not idempotent:\nfirst  = %#v\nsecond = %#v", first, second)
	}
}

// TestCodeInterprocEvidenceReplayIsIdempotent mirrors
// TestCodeTaintEvidenceReplayIsIdempotent for the cross-function family.
func TestCodeInterprocEvidenceReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "interproc-a",
			FactKind: "code_interproc_evidence",
			Payload: map[string]any{
				"source_function_uid": "uid:source-a",
				"sink_function_uid":   "uid:sink-a",
				"repo_id":             "repo-a",
				"relative_path":       "src/a.go",
				"sink_kind":           "sql_exec",
				"source_kind":         "http_request",
				"confidence":          0.7,
				"why_trail": []map[string]any{
					{"role": "source", "function_id": "a"},
					{"role": "sink", "function_id": "b"},
				},
			},
		},
	}

	replay := func() []map[string]any {
		rows, _, err := ExtractCodeInterprocEvidenceRowsWithQuarantine(envelopes)
		if err != nil {
			t.Fatalf("ExtractCodeInterprocEvidenceRowsWithQuarantine error = %v, want nil", err)
		}
		return rows
	}

	first := replay()
	second := replay()

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("len(first)=%d len(second)=%d, want 1 row each", len(first), len(second))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replay is not idempotent:\nfirst  = %#v\nsecond = %#v", first, second)
	}
}

// TestExtractShellExecRowsReplayIsIdempotent proves the shell-exec identity
// conversion (decodeCodegraphRepository/decodeCodegraphFile) produces
// byte-identical rows across repeated extraction of the same envelope batch,
// the same replay-safety contract the taint/interproc tests above establish.
func TestExtractShellExecRowsReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "repo-shell",
			FactKind: factKindRepository,
			Payload:  map[string]any{"repo_id": "repo-shell"},
		},
		{
			FactID:   "file-shell",
			FactKind: factKindFile,
			Payload: map[string]any{
				"repo_id":       "repo-shell",
				"relative_path": "cmd/tool/main.go",
				"parsed_file_data": map[string]any{
					"path": "/repo/cmd/tool/main.go",
					"functions": []any{
						map[string]any{"name": "runTool", "line_number": 7, "uid": "function:runTool"},
					},
					"embedded_shell_commands": []any{
						map[string]any{
							"function_name":        "runTool",
							"function_line_number": 7,
							"line_number":          8,
							"api":                  "os/exec.CommandContext",
							"language":             "go",
						},
					},
				},
			},
		},
	}

	firstRepoIDs, firstRows := ExtractShellExecRows(envelopes)
	secondRepoIDs, secondRows := ExtractShellExecRows(envelopes)

	if !reflect.DeepEqual(firstRepoIDs, secondRepoIDs) {
		t.Fatalf("repoIDs replay is not idempotent: first=%v second=%v", firstRepoIDs, secondRepoIDs)
	}
	if !reflect.DeepEqual(firstRows, secondRows) {
		t.Fatalf("rows replay is not idempotent:\nfirst  = %#v\nsecond = %#v", firstRows, secondRows)
	}
	if len(firstRows) != 1 {
		t.Fatalf("len(firstRows) = %d, want 1", len(firstRows))
	}
}
