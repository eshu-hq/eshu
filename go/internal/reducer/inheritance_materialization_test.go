// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestInheritanceMaterializationHandlerRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := InheritanceMaterializationHandler{
		FactLoader:   &stubFactLoader{},
		IntentWriter: &recordingInheritanceIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainCodeCallMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for mismatched domain, got nil")
	}
}

func TestInheritanceMaterializationHandlerRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := InheritanceMaterializationHandler{
		IntentWriter: &recordingInheritanceIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for nil fact loader, got nil")
	}
}

func TestInheritanceMaterializationHandlerRequiresIntentWriter(t *testing.T) {
	t.Parallel()

	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for nil intent writer, got nil")
	}
}

func TestExtractInheritanceRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	repoIDs, rows := ExtractInheritanceRows(nil)
	if repoIDs != nil {
		t.Fatalf("repoIDs = %v, want nil", repoIDs)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
}

func TestExtractInheritanceRowsNoBasesReturnsEmpty(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":         "repo-1",
				"entity_id":       "content-entity:e_child",
				"entity_type":     "Class",
				"entity_name":     "ChildClass",
				"file_path":       "/src/child.py",
				"language":        "python",
				"start_line":      10,
				"end_line":        50,
				"entity_metadata": map[string]any{
					// no bases key
				},
			},
		},
	}

	repoIDs, rows := ExtractInheritanceRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-1" {
		t.Fatalf("repoIDs = %v, want [repo-1]", repoIDs)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestExtractInheritanceRowsFromClassWithBases(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_parent",
				"entity_type": "Class",
				"entity_name": "ParentClass",
				"file_path":   "/src/parent.py",
				"language":    "python",
				"start_line":  1,
				"end_line":    30,
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_child",
				"entity_type": "Class",
				"entity_name": "ChildClass",
				"file_path":   "/src/child.py",
				"language":    "python",
				"start_line":  10,
				"end_line":    50,
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}

	repoIDs, rows := ExtractInheritanceRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-1" {
		t.Fatalf("repoIDs = %v, want [repo-1]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["child_entity_id"], "content-entity:e_child"; got != want {
		t.Fatalf("child_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["parent_entity_id"], "content-entity:e_parent"; got != want {
		t.Fatalf("parent_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["repo_id"], "repo-1"; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "INHERITS"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["child_entity_type"], "Class"; got != want {
		t.Fatalf("child_entity_type = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["parent_entity_type"], "Class"; got != want {
		t.Fatalf("parent_entity_type = %#v, want %#v", got, want)
	}
}

func TestExtractInheritanceRowsFromInterfaceWithBases(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_base_iface",
				"entity_type": "Interface",
				"entity_name": "BaseInterface",
				"file_path":   "/src/base.go",
				"language":    "go",
				"start_line":  1,
				"end_line":    10,
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_child_iface",
				"entity_type": "Interface",
				"entity_name": "ChildInterface",
				"file_path":   "/src/child.go",
				"language":    "go",
				"start_line":  12,
				"end_line":    20,
				"entity_metadata": map[string]any{
					"bases": []any{"BaseInterface"},
				},
			},
		},
	}

	_, rows := ExtractInheritanceRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["child_entity_id"], "content-entity:e_child_iface"; got != want {
		t.Fatalf("child_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["parent_entity_id"], "content-entity:e_base_iface"; got != want {
		t.Fatalf("parent_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "INHERITS"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
}

func TestExtractInheritanceRowsDeduplicates(t *testing.T) {
	t.Parallel()

	// Two entities with same name in different files -- only one parent exists.
	// The child references "ParentClass" once, so only one edge should appear.
	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_parent",
				"entity_type": "Class",
				"entity_name": "ParentClass",
				"file_path":   "/src/parent.py",
				"language":    "python",
				"start_line":  1,
				"end_line":    30,
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_child_a",
				"entity_type": "Class",
				"entity_name": "ChildA",
				"file_path":   "/src/child_a.py",
				"language":    "python",
				"start_line":  1,
				"end_line":    20,
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_child_b",
				"entity_type": "Class",
				"entity_name": "ChildB",
				"file_path":   "/src/child_b.py",
				"language":    "python",
				"start_line":  1,
				"end_line":    20,
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}

	_, rows := ExtractInheritanceRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (one per child)", len(rows))
	}

	// Verify dedup: same child->parent pair should not be duplicated.
	seen := make(map[string]struct{})
	for _, row := range rows {
		key := row["child_entity_id"].(string) + "->" + row["parent_entity_id"].(string)
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate edge found: %s", key)
		}
		seen[key] = struct{}{}
	}
}

func TestExtractInheritanceRowsSkipsUnresolvedBases(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_child",
				"entity_type": "Class",
				"entity_name": "ChildClass",
				"file_path":   "/src/child.py",
				"language":    "python",
				"start_line":  10,
				"end_line":    50,
				"entity_metadata": map[string]any{
					"bases": []any{"UnknownParent"},
				},
			},
		},
	}

	_, rows := ExtractInheritanceRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unresolved base", len(rows))
	}
}

func TestInheritanceMaterializationHandlerEmitsIntents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	writer := &recordingInheritanceIntentWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: inheritanceEntityFacts()},
		IntentWriter: writer,
	}

	intent := Intent{
		IntentID:        "intent-inheritance-1",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainInheritanceMaterialization,
		Cause:           "inheritance materialization follow-up",
		EntityKeys:      []string{"repo-1"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	// One refresh intent + one per-edge intent.
	if result.CanonicalWrites != 2 {
		t.Fatalf("result.CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}

	refresh := writer.refreshRows()
	if len(refresh) != 1 {
		t.Fatalf("refresh intents = %d, want 1", len(refresh))
	}
	if refresh[0].PartitionKey != inheritanceWholeScopePartitionKey("repo-1") {
		t.Fatalf("refresh partition key = %q, want whole-scope fence key", refresh[0].PartitionKey)
	}
	if refresh[0].SourceRunID != "run-1" {
		t.Fatalf("refresh source_run_id = %q, want run-1", refresh[0].SourceRunID)
	}

	edges := writer.edgeRows()
	if len(edges) != 1 {
		t.Fatalf("per-edge intents = %d, want 1", len(edges))
	}
	if !rowUsesRefreshFence(edges[0]) {
		t.Fatal("per-edge intent is not marked retract_via_refresh")
	}
	if got, want := edges[0].Payload["child_entity_id"], "content-entity:e_child"; got != want {
		t.Fatalf("child_entity_id = %#v, want %#v", got, want)
	}
	if got, want := edges[0].Payload["parent_entity_id"], "content-entity:e_parent"; got != want {
		t.Fatalf("parent_entity_id = %#v, want %#v", got, want)
	}
	if got, want := edges[0].Payload["child_path"], "/repo/child.py"; got != want {
		t.Fatalf("child_path = %#v, want %#v", got, want)
	}
	wantPartition := inheritanceFilePartitionKey("repo-1", "/repo/child.py", "content-entity:e_child->content-entity:e_parent:INHERITS")
	if edges[0].PartitionKey != wantPartition {
		t.Fatalf("per-edge partition key = %q, want %q", edges[0].PartitionKey, wantPartition)
	}
}

func TestInheritanceMaterializationHandlerNoEntitiesSucceeds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	writer := &recordingInheritanceIntentWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: []facts.Envelope{}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("result.CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("emitted %d intents, want 0", len(writer.rows))
	}
}

// --- test doubles ---

func inheritanceEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: factKindRepository,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"path":          "/repo",
				"source_run_id": "run-1",
			},
		},
		{
			FactKind: "content_entity",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_parent",
				"entity_type": "Class",
				"entity_name": "ParentClass",
				"path":        "/repo/parent.py",
			},
		},
		{
			FactKind: "content_entity",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_child",
				"entity_type": "Class",
				"entity_name": "ChildClass",
				"path":        "/repo/child.py",
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}
}
