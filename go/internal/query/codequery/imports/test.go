// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// stubGraph is a canned GraphQuery for dispatch tests.
type stubGraph struct {
	rows []map[string]any
	err  error
}

func (s stubGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return s.rows, s.err
}

func (s stubGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, s.err
}

func TestParamsBindsPagingAndSelectors(t *testing.T) {
	req := codemodel.ImportDependencyRequest{RepoID: " repo-a ", SourceFile: "a.py"}
	params := Params(req)
	if params["repo_id"] != "repo-a" {
		t.Fatalf("repo_id = %v, want trimmed", params["repo_id"])
	}
	if params["source_file"] != "a.py" {
		t.Fatalf("source_file = %v, want set", params["source_file"])
	}
	if _, ok := params["limit"]; !ok {
		t.Fatal("limit missing, want paged")
	}
}

func TestUniqueScopesDedupesAndOrders(t *testing.T) {
	rows := []map[string]any{
		{"repo_id": "b", "path": "y.py"},
		{"repo_id": "a", "path": "x.py"},
		{"repo_id": "a", "path": "x.py"},
		{"repo_id": "", "path": "z.py"},
	}
	scopes := UniqueScopes(rows, "path")
	if len(scopes) != 2 {
		t.Fatalf("len(scopes) = %d, want 2 deduplicated", len(scopes))
	}
	if scopes[0]["repo_id"] != "a" {
		t.Fatalf("first scope repo = %v, want stable repo order", scopes[0]["repo_id"])
	}
}

func TestScopePathsListsDistinctSorted(t *testing.T) {
	scopes := []map[string]any{{"path": "b.py"}, {"path": "a.py"}, {"path": "a.py"}}
	paths := ScopePaths(scopes)
	if len(paths) != 2 || paths[0] != "a.py" || paths[1] != "b.py" {
		t.Fatalf("paths = %v, want [a.py b.py]", paths)
	}
}

func TestModuleScopesEmptyModuleReadsNothing(t *testing.T) {
	scopes, err := ModuleScopes(context.Background(), nil, codemodel.ImportDependencyRequest{}, true)
	if err != nil || scopes != nil {
		t.Fatalf("ModuleScopes(empty) = %v, %v; want nil, nil (nil graph untouched)", scopes, err)
	}
}

func TestRowsDispatchesDirectRead(t *testing.T) {
	want := []map[string]any{{"source_path": "a.py"}}
	rows, err := Rows(context.Background(), stubGraph{rows: want}, codemodel.ImportDependencyRequest{})
	if err != nil {
		t.Fatalf("Rows = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want the stubbed direct read", len(rows))
	}
}
