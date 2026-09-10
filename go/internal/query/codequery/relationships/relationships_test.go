// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestNormalizeDirectionAcceptsKnown(t *testing.T) {
	for _, direction := range []string{"", "incoming", "outgoing", "INCOMING"} {
		if _, err := NormalizeDirection(direction); err != nil {
			t.Fatalf("NormalizeDirection(%q) = %v, want nil", direction, err)
		}
	}
	if _, err := NormalizeDirection("sideways"); err == nil {
		t.Fatal("NormalizeDirection(sideways) = nil, want error")
	}
}

func TestCapabilityResolvesFamilies(t *testing.T) {
	if got := Capability("incoming", "CALLS"); got != "call_graph.direct_callers" {
		t.Fatalf("Capability = %q, want direct_callers", got)
	}
	if got := Capability("outgoing", "IMPORTS"); got != "symbol_graph.imports" {
		t.Fatalf("Capability = %q, want imports", got)
	}
	if got := TransitiveCapability("incoming"); got != "call_graph.transitive_callers" {
		t.Fatalf("TransitiveCapability = %q, want transitive_callers", got)
	}
}

func TestFilterResponseDropsUnaskedDirection(t *testing.T) {
	response := map[string]any{
		"outgoing": []map[string]any{{"type": "CALLS"}},
		"incoming": []map[string]any{{"type": "CALLS"}},
	}
	filtered := FilterResponse(response, "incoming", "CALLS")
	if len(filtered["outgoing"].([]map[string]any)) != 0 {
		t.Fatal("outgoing kept for an incoming filter, want dropped")
	}
	if len(filtered["incoming"].([]map[string]any)) != 1 {
		t.Fatal("incoming dropped for an incoming filter, want kept")
	}
}

func TestFilterRelationshipsMatchesCaseInsensitively(t *testing.T) {
	rows := []map[string]any{{"type": "calls"}, {"type": "IMPORTS"}}
	if got := FilterRelationships(rows, "CALLS"); len(got) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(got))
	}
	if got := FilterRelationships(rows, ""); len(got) != 2 {
		t.Fatalf("len(unfiltered) = %d, want 2", len(got))
	}
}

func TestRelationshipPatternGatesUnknownTypes(t *testing.T) {
	if got := NornicDBRelationshipPattern("CALLS"); got != ":CALLS" {
		t.Fatalf("pattern = %q, want :CALLS", got)
	}
	if got := NornicDBRelationshipPattern("CALLS OR 1=1"); got != "" {
		t.Fatalf("pattern = %q for injection input, want empty", got)
	}
}

func TestNodePatternAnchorsProperty(t *testing.T) {
	got := NornicDBNodePatternWithProperty("e", "Function", "uid", "$entity_id")
	want := "(e:Function {uid: $entity_id})"
	if got != want {
		t.Fatalf("pattern = %q, want %q", got, want)
	}
}

func TestTransitiveRowsWalksFrontier(t *testing.T) {
	hops := map[string][]map[string]any{
		"a": {{"source_id": "b"}},
		"b": {{"source_id": "c"}},
		"c": {},
	}
	stub := func(_ context.Context, id string, _ string) ([]map[string]any, error) {
		return hops[id], nil
	}
	rows, err := TransitiveRows(context.Background(), "a", "incoming", 5, stub)
	if err != nil {
		t.Fatalf("TransitiveRows = %v, want nil", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 frontier hops", len(rows))
	}
	if rows[0]["depth"] != 1 || rows[1]["depth"] != 2 {
		t.Fatalf("depths = %v/%v, want 1/2", rows[0]["depth"], rows[1]["depth"])
	}
}

func TestInheritanceRowsInGrantDropsUngrantedInterior(t *testing.T) {
	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}}
	rows := []map[string]any{
		{"id": "x", "path_nodes": []any{map[string]any{"repo_id": "repo-a"}, map[string]any{"repo_id": "repo-b"}}},
		{"id": "y", "path_nodes": []any{map[string]any{"repo_id": "repo-a"}}},
		{"id": "z"},
	}
	kept := NornicDBInheritanceRowsInGrant(rows, access)
	if len(kept) != 1 {
		t.Fatalf("len(kept) = %d, want only the granted-interior row", len(kept))
	}
}
