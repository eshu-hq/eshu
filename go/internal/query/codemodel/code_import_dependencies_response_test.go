// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"testing"
)

// TestImportDependencyResponseShapes proves each query family lands on
// its own response key with the shared paging envelope.
func TestImportDependencyResponseShapes(t *testing.T) {
	t.Parallel()

	row := map[string]any{"repo_id": "repo-1", "source_file": "a.go", "target_module": "b"}
	cases := []struct {
		queryType string
		key       string
	}{
		{"imports_by_file", "dependencies"},
		{"file_import_cycles", "cycles"},
		{"cross_module_calls", "cross_module_calls"},
		{"package_imports", "modules"},
	}
	for _, tc := range cases {
		t.Run(tc.queryType, func(t *testing.T) {
			t.Parallel()
			req := ImportDependencyRequest{QueryType: tc.queryType, RepoID: "repo-1"}
			response := ImportDependencyResponse(req, []map[string]any{row})
			if _, ok := response[tc.key]; !ok {
				t.Errorf("response for %q missing key %q: %#v", tc.queryType, tc.key, response)
			}
			if got := response["query_type"]; got != tc.queryType {
				t.Errorf("query_type = %v, want %q", got, tc.queryType)
			}
			if got := response["count"]; got != 1 {
				t.Errorf("count = %v, want 1", got)
			}
		})
	}
}

// TestImportDependencyResponseTruncation proves the limit+1 probe sets
// has_more with a next cursor, while a capped cycle enumeration stays
// truncated on its last page without one.
func TestImportDependencyResponseTruncation(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"a": 1}, {"a": 2}, {"a": 3}}
	paged := ImportDependencyResponse(ImportDependencyRequest{Limit: 1}, rows)
	if paged["has_more"] != true || paged["truncated"] != true {
		t.Errorf("over-limit page must be has_more+truncated: %#v", paged)
	}
	if paged["next_offset"] != 1 {
		t.Errorf("next_offset = %v, want 1", paged["next_offset"])
	}

	last := ImportDependencyResponseWithCycleEnumeration(
		ImportDependencyRequest{QueryType: "file_import_cycles"},
		[]map[string]any{{"source_file": "a.py", "target_file": "b.py"}},
		CycleEnumeration{Truncated: true, StopReason: CycleStopCycleCap},
	)
	if last["truncated"] != true {
		t.Errorf("capped last page must stay truncated: %#v", last)
	}
	if last["has_more"] != false {
		t.Errorf("capped last page must end the pager: %#v", last)
	}
	if last["next_offset"] != nil {
		t.Errorf("capped last page next_offset = %v, want nil", last["next_offset"])
	}
}

// TestImportDependencyUniqueModules proves module rows deduplicate on
// the repository/module/language key and skip module-less rows.
func TestImportDependencyUniqueModules(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"repo_id": "repo-1", "target_module": "b", "language": "go"},
		{"repo_id": "repo-1", "target_module": "b", "language": "go"},
		{"repo_id": "repo-1", "language": "go"},
	}
	modules := ImportDependencyUniqueModules(rows)
	if len(modules) != 1 {
		t.Fatalf("len(modules) = %d, want 1", len(modules))
	}
	if got := modules[0]["module"]; got != "b" {
		t.Errorf("module = %v, want b", got)
	}
}

// TestImportDependencyCoverageShapes proves the coverage block names
// the query shape per family, including the per-language cycle lifts.
func TestImportDependencyCoverageShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		req        ImportDependencyRequest
		queryShape string
	}{
		{"plain", ImportDependencyRequest{}, "repo_file_imports"},
		{"python cycles", ImportDependencyRequest{QueryType: "file_import_cycles"}, "python_file_import_cycle"},
		{"go cycles", ImportDependencyRequest{QueryType: "file_import_cycles", Language: "go"}, "go_package_import_cycle"},
		{"ts cycles", ImportDependencyRequest{QueryType: "file_import_cycles", Language: "typescript"}, "ts_package_import_cycle"},
		{"calls", ImportDependencyRequest{QueryType: "cross_module_calls"}, "module_anchored_call_edges"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			response := ImportDependencyResponse(tc.req, nil)
			coverage, ok := response["coverage"].(map[string]any)
			if !ok {
				t.Fatalf("coverage block missing: %#v", response)
			}
			if got := coverage["query_shape"]; got != tc.queryShape {
				t.Errorf("query_shape = %v, want %q", got, tc.queryShape)
			}
		})
	}
}
