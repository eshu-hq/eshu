// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func importCyclePythonRow(sourceFile, targetModule string, line int) map[string]any {
	return map[string]any{
		"repo_id":       "repo-1",
		"repo_name":     "repo",
		"source_path":   "p/" + sourceFile,
		"source_file":   sourceFile,
		"source_name":   sourceFile,
		"language":      "python",
		"target_module": targetModule,
		"line_number":   line,
	}
}

// TestBuildFileImportCycleRowsFindsPythonCycle proves the bounded
// enumeration closes a reciprocal Python import pair into one cycle row.
func TestBuildFileImportCycleRowsFindsPythonCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		importCyclePythonRow("a.py", "b", 3),
		importCyclePythonRow("b.py", "a", 7),
	}
	rows, enumeration, err := BuildFileImportCycleRows(ImportDependencyRequest{}, edges)
	if err != nil {
		t.Fatalf("BuildFileImportCycleRows failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 closed cycle", len(rows))
	}
	if enumeration.Truncated {
		t.Error("two-edge enumeration must not report truncation")
	}
}

// TestBuildFileImportCycleRowsRejectsOverScan proves an over-limit
// candidate scan fails closed before enumeration runs.
func TestBuildFileImportCycleRowsRejectsOverScan(t *testing.T) {
	t.Parallel()

	_, _, err := BuildFileImportCycleRows(
		ImportDependencyRequest{},
		make([]map[string]any, querycontract.ImportDependencyInternalScanLimit+1),
	)
	if !errors.Is(err, ErrImportDependencyScopeTooBroad) {
		t.Errorf("over-scan error = %v, want %v", err, ErrImportDependencyScopeTooBroad)
	}
}

// TestFilterCrossModuleCallRows proves cross-repository candidates are
// dropped, the survivors are ordered and paged, and the page sheds its
// internal routing keys.
func TestFilterCrossModuleCallRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{
			"source_repo_id": "repo-1", "target_repo_id": "repo-1",
			"source_file": "b.go", "target_file": "c.go",
			"source_id": "s2", "target_id": "t2",
			"source_path": "p/b.go", "target_path": "p/c.go",
			"source_language": "go", "target_language": "go",
		},
		{
			"source_repo_id": "repo-1", "target_repo_id": "repo-2",
			"source_file": "a.go", "target_file": "z.go",
			"source_id": "s1", "target_id": "t1",
		},
	}
	page, err := FilterCrossModuleCallRows(ImportDependencyRequest{}, rows, nil, nil)
	if err != nil {
		t.Fatalf("FilterCrossModuleCallRows failed: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("len(page) = %d, want 1 same-repo row", len(page))
	}
	if got := page[0]["repo_id"]; got != "repo-1" {
		t.Errorf("page repo_id = %v, want repo-1", got)
	}
	for _, internal := range []string{"source_repo_id", "target_repo_id", "source_path", "target_path"} {
		if _, ok := page[0][internal]; ok {
			t.Errorf("page must shed internal key %q", internal)
		}
	}
}

// TestFilterImportDependencyScopeRows proves scope filtering keeps only
// rows inside the listed repository paths and passes everything when
// no scopes are given.
func TestFilterImportDependencyScopeRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"repo_id": "repo-1", "path": "a.go"},
		{"repo_id": "repo-1", "path": "b.go"},
	}
	scopes := []map[string]any{{"repo_id": "repo-1", "path": "a.go"}}
	filtered := FilterImportDependencyScopeRows(rows, "repo_id", "path", scopes)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if got := FilterImportDependencyScopeRows(rows, "repo_id", "path", nil); len(got) != 2 {
		t.Errorf("nil scopes must pass %d rows through, got %d", len(rows), len(got))
	}
}

// TestUniquePackageImportRows proves module rows deduplicate on the
// repository/module/language key and sort deterministically.
func TestUniquePackageImportRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"repo_id": "repo-1", "target_module": "b", "language": "go"},
		{"repo_id": "repo-1", "target_module": "a", "language": "go"},
		{"repo_id": "repo-1", "target_module": "a", "language": "go"},
	}
	unique := UniquePackageImportRows(rows)
	if len(unique) != 2 {
		t.Fatalf("len(unique) = %d, want 2", len(unique))
	}
	if got := unique[0]["target_module"]; got != "a" {
		t.Errorf("unique[0].target_module = %v, want a (sorted)", got)
	}
}

// TestPageImportDependencyRows proves the limit+1 truncation probe
// window and the past-the-end empty page.
func TestPageImportDependencyRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"a": 1}, {"a": 2}, {"a": 3}}
	limit := 1
	page := PageImportDependencyRows(ImportDependencyRequest{Limit: limit}, rows)
	if len(page) != 2 {
		t.Errorf("len(page) = %d, want the limit+1 probe window 2", len(page))
	}
	if got := PageImportDependencyRows(ImportDependencyRequest{Offset: 9}, rows); len(got) != 0 {
		t.Errorf("past-the-end page returned %d rows, want 0", len(got))
	}
}

// TestStripImportDependencyInternalPaths proves the shaper removes the
// internal routing paths before the response is rendered.
func TestStripImportDependencyInternalPaths(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"source_path": "a", "target_path": "b", "repo_id": "r"}}
	StripImportDependencyInternalPaths(rows)
	for _, internal := range []string{"source_path", "target_path"} {
		if _, ok := rows[0][internal]; ok {
			t.Errorf("row must shed internal key %q", internal)
		}
	}
}
