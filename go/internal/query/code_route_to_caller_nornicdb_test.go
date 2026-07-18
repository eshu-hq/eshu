// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestCodeEntityLabelAllowedGatesInterpolatedLabel guards the #5287 fix: only a
// known code-entity label may be folded into the handler-anchored traversal, so
// a graph-supplied label can never inject Cypher.
func TestCodeEntityLabelAllowedGatesInterpolatedLabel(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"Function", "Class", "Struct", "Interface", "TypeAlias", "File"} {
		if !codeEntityLabelAllowed(label) {
			t.Errorf("codeEntityLabelAllowed(%q) = false, want true", label)
		}
	}
	for _, label := range []string{"", "Repository", "Workload", "Function) DETACH DELETE (n"} {
		if codeEntityLabelAllowed(label) {
			t.Errorf("codeEntityLabelAllowed(%q) = true, want false", label)
		}
	}
}

// TestRouteToCallerEntityFromChainDecodesBothBackends proves the far-endpoint
// extractor reads the last node of a nodes(path) chain from both the Neo4j
// driver shape (neo4j.Node) and the NornicDB map shape.
func TestRouteToCallerEntityFromChainDecodesBothBackends(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"entity_id": "fn-1", "name": "handleOrders", "file_path": "app.py",
		"repo_id": "repo-1", "language": "python", "start_line": 10, "end_line": 20,
	}
	props := map[string]any{
		"id": "fn-1", "name": "handleOrders", "file_path": "app.py",
		"repo_id": "repo-1", "language": "python", "start_line": int64(10), "end_line": int64(20),
	}
	cases := map[string]any{
		// The known handler is the first node; the discovered entity is the last.
		"neo4j-node": []any{
			neo4jdriver.Node{Props: map[string]any{"id": "handler"}},
			neo4jdriver.Node{Props: props},
		},
		"nornicdb-map": []any{
			map[string]any{"id": "handler"},
			props,
		},
	}
	for name, chain := range cases {
		got := routeToCallerEntityFromChain(chain)
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: entity[%q] = %#v, want %#v", name, k, got[k], v)
			}
		}
	}
	if routeToCallerEntityFromChain([]any{}) != nil {
		t.Error("empty chain should decode to nil")
	}
	if routeToCallerEntityFromChain(nil) != nil {
		t.Error("non-list chain should decode to nil")
	}
}

// TestRouteToCallerEntityFromChainPrefersUidAndRelativePath covers the id/uid and
// file_path/relative_path fallbacks.
func TestRouteToCallerEntityFromChainPrefersUidAndRelativePath(t *testing.T) {
	t.Parallel()

	got := routeToCallerEntityFromChain([]any{
		map[string]any{"uid": "u-1", "relative_path": "pkg/app.py"},
	})
	if got["entity_id"] != "u-1" {
		t.Errorf("entity_id = %#v, want u-1 (uid fallback)", got["entity_id"])
	}
	if got["file_path"] != "pkg/app.py" {
		t.Errorf("file_path = %#v, want pkg/app.py (relative_path fallback)", got["file_path"])
	}
}
