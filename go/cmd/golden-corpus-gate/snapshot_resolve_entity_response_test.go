// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// resolveEntityPostAliasBody is a resolve_entity payload as the API emits it
// after #7173: entities carries both rows, and there is no `matches` field. It
// is written out by hand, not derived from the snapshot shape, so a shape that
// still pins a removed path cannot make its own fixture.
const resolveEntityPostAliasBody = `{
  "entities": [
    {"id": "content-entity:e_85e904a13eae", "entity_id": "content-entity:e_85e904a13eae", "name": "main", "labels": ["Function"], "repo_id": "repository:r_a", "file_path": "cmd/a/main.go"},
    {"id": "content-entity:e_85bff2c7884a", "entity_id": "content-entity:e_85bff2c7884a", "name": "main", "labels": ["Function"], "repo_id": "repository:r_b", "file_path": "main.go"}
  ],
  "count": 2,
  "limit": 10,
  "truncated": false
}`

// TestGoldenSnapshotResolveEntityShapeAcceptsTheAliasFreeResponse runs the
// committed resolve_entity shape through the gate's own evaluator. The unit
// tests that only read the snapshot are self-consistent with a stale pin; this
// one fails when the shape still requires a path the response no longer has,
// which is how a leftover `matches[].id` would have gone red only in the live
// gate (#7173).
func TestGoldenSnapshotResolveEntityShapeAcceptsTheAliasFreeResponse(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.MCP["resolve_entity"]
	if !ok {
		t.Fatal("query_shapes.mcp missing resolve_entity")
	}
	if finding := EvaluateQueryShape("resolve_entity", shape, []byte(resolveEntityPostAliasBody)); !finding.OK {
		t.Fatalf("resolve_entity shape rejects the alias-free response: %s", finding.Detail)
	}

	legacy := shape
	legacy.RequiredJSONValues = map[string]any{}
	for key, value := range shape.RequiredJSONValues {
		legacy.RequiredJSONValues[key] = value
	}
	legacy.RequiredJSONValues["matches[].id"] = "content-entity:e_85bff2c7884a"
	if finding := EvaluateQueryShape("resolve_entity-legacy", legacy, []byte(resolveEntityPostAliasBody)); finding.OK {
		t.Fatal("a shape pinning matches[].id passed against a response without matches; the evaluator no longer catches a stale path pin")
	}
}
