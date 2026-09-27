// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// Multi-node cycle handler proof for #6851: the HTTP route serves the
// bounded enumeration with its cap metadata, and rejects an out-of-range
// max_cycle_length before any graph read runs.

func TestHandleFileImportCyclesServesBoundedEnumeration(t *testing.T) {
	t.Parallel()

	calls := 0
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{run: func(
			_ context.Context,
			_ string,
			_ map[string]any,
		) ([]map[string]any, error) {
			calls++
			return []map[string]any{
				{"repo_id": "repo-1", "repo_name": "platform", "source_path": "/proof/src/a.py", "source_file": "src/a.py", "source_name": "a.py", "language": "python", "target_module": "b", "line_number": 3},
				{"repo_id": "repo-1", "repo_name": "platform", "source_path": "/proof/src/b.py", "source_file": "src/b.py", "source_name": "b.py", "language": "python", "target_module": "c", "line_number": 5},
				{"repo_id": "repo-1", "repo_name": "platform", "source_path": "/proof/src/c.py", "source_file": "src/c.py", "source_name": "c.py", "language": "python", "target_module": "a", "line_number": 7},
			}, nil
		}},
	}

	response := serveImportDependencyRequest(t, handler,
		`{"query_type":"file_import_cycles","repo_id":"repo-1","limit":25,"max_cycle_length":5}`)
	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
	if got, want := calls, 1; got != want {
		t.Fatalf("graph calls = %d, want one bounded edge scan", got)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	cycles, ok := body["cycles"].([]any)
	if !ok || len(cycles) != 1 {
		t.Fatalf("cycles = %#v, want one 3-node cycle", body["cycles"])
	}
	cycle, ok := cycles[0].(map[string]any)
	if !ok {
		t.Fatalf("cycle = %#v, want an object", cycles[0])
	}
	if got := cycle["cycle_length"]; got != float64(3) {
		t.Fatalf("cycle_length = %#v, want 3", got)
	}
	coverage, ok := body["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage = %#v, want a coverage map", body["coverage"])
	}
	if got := coverage["cycle_max_length"]; got != float64(5) {
		t.Fatalf("coverage.cycle_max_length = %#v, want 5", got)
	}
	if got := coverage["cycle_enumeration_cap"]; got != float64(1000) {
		t.Fatalf("coverage.cycle_enumeration_cap = %#v, want 1000", got)
	}
	if got := coverage["cycle_enumeration_truncated"]; got != false {
		t.Fatalf("coverage.cycle_enumeration_truncated = %#v, want false", got)
	}
	scope, ok := body["scope"].(map[string]any)
	if !ok {
		t.Fatalf("scope = %#v, want a scope map", body["scope"])
	}
	if got := scope["max_cycle_length"]; got != float64(5) {
		t.Fatalf("scope.max_cycle_length = %#v, want 5", got)
	}
}

func TestHandleFileImportCyclesRejectsBadMaxCycleLength(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		_ string,
		_ map[string]any,
	) ([]map[string]any, error) {
		t.Fatal("graph read ran for an invalid max_cycle_length")
		return nil, nil
	}}}

	response := serveImportDependencyRequest(t, handler,
		`{"query_type":"file_import_cycles","repo_id":"repo-1","max_cycle_length":99}`)
	if got, want := response.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
}
