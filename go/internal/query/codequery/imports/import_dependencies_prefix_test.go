// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// captureGraph records the Cypher and params of the last run.
type captureGraph struct {
	rows   []map[string]any
	cypher string
	params map[string]any
}

func (g *captureGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.cypher = cypher
	g.params = params
	return g.rows, nil
}

func (g *captureGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func prefixString(value string) *string {
	return &value
}

// TestImportRowsRoutesPrefixToPrefixBuilder proves a prefix request runs
// the new STARTS WITH builder and binds the trimmed prefix parameter.
func TestImportRowsRoutesPrefixToPrefixBuilder(t *testing.T) {
	graph := &captureGraph{rows: []map[string]any{}}
	req := codemodel.ImportDependencyRequest{
		TargetModulePrefix: prefixString(" @scope/ "),
	}
	if _, err := ImportRows(context.Background(), graph, req); err != nil {
		t.Fatalf("ImportRows(prefix) error = %v, want nil", err)
	}
	if !strings.Contains(graph.cypher, "target_module.name STARTS WITH $target_module_prefix") {
		t.Fatalf("prefix read cypher lacks the prefix predicate:\n%s", graph.cypher)
	}
	if got := graph.params["target_module_prefix"]; got != "@scope/" {
		t.Fatalf("target_module_prefix param = %#v, want trimmed %q", got, "@scope/")
	}
	if strings.Contains(graph.cypher, "{name: $target_module}") {
		t.Fatalf("prefix read cypher must not carry the exact-match node:\n%s", graph.cypher)
	}
}

// TestImportRowsRoutesExternalOnlyToPrefixBuilder proves external_only
// without a prefix runs the new builder with the exclusion predicates.
func TestImportRowsRoutesExternalOnlyToPrefixBuilder(t *testing.T) {
	graph := &captureGraph{rows: []map[string]any{}}
	req := codemodel.ImportDependencyRequest{RepoID: "repo-1", ExternalOnly: true}
	if _, err := ImportRows(context.Background(), graph, req); err != nil {
		t.Fatalf("ImportRows(external_only) error = %v, want nil", err)
	}
	for _, want := range []string{
		"NOT target_module.name STARTS WITH '.'",
		"NOT target_module.name STARTS WITH '/'",
	} {
		if !strings.Contains(graph.cypher, want) {
			t.Fatalf("external-only read cypher lacks %q:\n%s", want, graph.cypher)
		}
	}
	if _, ok := graph.params["target_module_prefix"]; ok {
		t.Fatalf("external-only read must not bind a prefix param: %#v", graph.params)
	}
}

// TestImportRowsKeepsExactBuilderForPlainRequests proves a request without
// prefix or external_only still runs the pinned exact-match builder.
func TestImportRowsKeepsExactBuilderForPlainRequests(t *testing.T) {
	graph := &captureGraph{rows: []map[string]any{}}
	req := codemodel.ImportDependencyRequest{RepoID: "repo-1", TargetModule: "requests"}
	if _, err := ImportRows(context.Background(), graph, req); err != nil {
		t.Fatalf("ImportRows(exact) error = %v, want nil", err)
	}
	if !strings.Contains(graph.cypher, "{name: $target_module}") {
		t.Fatalf("plain read cypher lacks the exact-match node:\n%s", graph.cypher)
	}
	if strings.Contains(graph.cypher, "$target_module_prefix") {
		t.Fatalf("plain read cypher must not mention the prefix parameter:\n%s", graph.cypher)
	}
}

// TestImportRowsFiltersSourceModulePathInGo proves the pinned
// source-module scan keeps its text while the prefix filter applies in Go.
func TestImportRowsFiltersSourceModulePathInGo(t *testing.T) {
	rows := []map[string]any{
		{"repo_id": "r", "source_path": "a.py", "source_file": "a.py", "target_module": "@scope/keep"},
		{"repo_id": "r", "source_path": "a.py", "source_file": "a.py", "target_module": "./drop"},
	}
	graph := &captureGraph{rows: rows}
	req := codemodel.ImportDependencyRequest{
		SourceModule:       "a",
		ExternalOnly:       true,
		TargetModulePrefix: prefixString("@scope/"),
	}
	got, err := ImportRows(context.Background(), graph, req)
	if err != nil {
		t.Fatalf("ImportRows(source module + prefix) error = %v, want nil", err)
	}
	if strings.Contains(graph.cypher, "STARTS WITH") {
		t.Fatalf("source-module scan must keep its pinned text:\n%s", graph.cypher)
	}
	if len(got) != 1 {
		t.Fatalf("len(rows) = %d, want 1 after the Go-side prefix filter", len(got))
	}
	if module, _ := got[0]["target_module"].(string); module != "@scope/keep" {
		t.Fatalf("surviving target_module = %q, want the prefix-matching external module", module)
	}
}
