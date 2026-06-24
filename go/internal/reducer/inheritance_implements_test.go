// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractInheritanceRowsEmitsImplementsEdge(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-impl",
				"entity_id":   "content-entity:iface",
				"entity_type": "Interface",
				"entity_name": "GreetingService",
				"file_path":   "/src/GreetingService.java",
				"language":    "java",
				"start_line":  1,
				"end_line":    5,
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-impl",
				"entity_id":   "content-entity:impl",
				"entity_type": "Class",
				"entity_name": "GreetingServiceImpl",
				"file_path":   "/src/GreetingServiceImpl.java",
				"language":    "java",
				"start_line":  1,
				"end_line":    20,
				"entity_metadata": map[string]any{
					"implemented_interfaces": []any{"GreetingService"},
				},
			},
		},
	}

	_, rows := ExtractInheritanceRows(envelopes)
	var implements map[string]any
	for _, row := range rows {
		if row["relationship_type"] == "IMPLEMENTS" {
			implements = row
		}
	}
	if implements == nil {
		t.Fatalf("no IMPLEMENTS row emitted; rows=%#v", rows)
	}
	if got, want := implements["child_entity_id"], "content-entity:impl"; got != want {
		t.Errorf("child_entity_id = %#v, want %#v", got, want)
	}
	if got, want := implements["parent_entity_id"], "content-entity:iface"; got != want {
		t.Errorf("parent_entity_id = %#v, want %#v", got, want)
	}
	if got, want := implements["child_entity_type"], "Class"; got != want {
		t.Errorf("child_entity_type = %#v, want %#v", got, want)
	}
	if got, want := implements["parent_entity_type"], "Interface"; got != want {
		t.Errorf("parent_entity_type = %#v, want %#v", got, want)
	}
}

// TestExtractInheritanceRowsImplementsRequiresKnownInterface proves an
// implemented interface that does not resolve to a known entity does not
// fabricate an edge.
func TestExtractInheritanceRowsImplementsRequiresKnownInterface(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-impl",
				"entity_id":   "content-entity:impl",
				"entity_type": "Class",
				"entity_name": "Widget",
				"file_path":   "/src/Widget.java",
				"language":    "java",
				"start_line":  1,
				"end_line":    20,
				"entity_metadata": map[string]any{
					"implemented_interfaces": []any{"ExternalUnknownInterface"},
				},
			},
		},
	}

	_, rows := ExtractInheritanceRows(envelopes)
	for _, row := range rows {
		if row["relationship_type"] == "IMPLEMENTS" {
			t.Fatalf("unexpected IMPLEMENTS row for unresolved interface: %#v", row)
		}
	}
}
