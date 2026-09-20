// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inheritance

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

func TestInheritanceMaterializationHandlerRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := MaterializationHandler{
		FactLoader:   &stubFactLoader{},
		IntentWriter: &recordingInheritanceIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for mismatched domain, got nil")
	}
}

func TestInheritanceMaterializationHandlerRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := MaterializationHandler{
		IntentWriter: &recordingInheritanceIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducercontract.DomainInheritanceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for nil fact loader, got nil")
	}
}

func TestInheritanceMaterializationHandlerRequiresIntentWriter(t *testing.T) {
	t.Parallel()

	handler := MaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducercontract.DomainInheritanceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for nil intent writer, got nil")
	}
}

func TestExtractInheritanceRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	repoIDs, rows := ExtractRows(nil)
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

	repoIDs, rows := ExtractRows(envelopes)
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

	repoIDs, rows := ExtractRows(envelopes)
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

	_, rows := ExtractRows(envelopes)
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

	_, rows := ExtractRows(envelopes)
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

	_, rows := ExtractRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unresolved base", len(rows))
	}
}

func TestInheritanceMaterializationHandlerEmitsIntents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	writer := &recordingInheritanceIntentWriter{}
	handler := MaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: inheritanceEntityFacts()},
		IntentWriter: writer,
	}

	intent := reducercontract.Intent{
		IntentID:        "intent-inheritance-1",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          reducercontract.DomainInheritanceMaterialization,
		Cause:           "inheritance materialization follow-up",
		EntityKeys:      []string{"repo-1"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          reducercontract.IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, reducercontract.ResultStatusSucceeded)
	}
	// One refresh intent + one per-edge intent.
	if result.CanonicalWrites != 2 {
		t.Fatalf("result.CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}

	refresh := writer.refreshRows()
	if len(refresh) != 1 {
		t.Fatalf("refresh intents = %d, want 1", len(refresh))
	}
	if refresh[0].PartitionKey != WholeScopePartitionKey("repo-1") {
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
	handler := MaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: []facts.Envelope{}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducercontract.DomainInheritanceMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, reducercontract.ResultStatusSucceeded)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("result.CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("emitted %d intents, want 0", len(writer.rows))
	}
}

// TestExtractInheritanceRowsDuplicateParentNameIsOrderIndependent proves
// parent resolution does not depend on envelope order (issue #6850). The
// ruby_rails_app corpus carries three in-repo classes named "Base", and the
// fact loader orders envelopes by wall-clock observed_at, so first-seen-wins
// resolved "< Base" to a different parent run to run and rc-12 flipped
// between 22 and 23. Duplicate names always resolve to the smallest
// entity_id, so every envelope order emits the same edge.
func TestExtractInheritanceRowsDuplicateParentNameIsOrderIndependent(t *testing.T) {
	t.Parallel()

	parent := func(id string) facts.Envelope {
		return facts.Envelope{
			FactKind: "content_entity",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     id,
				"entity_type":   "Class",
				"entity_name":   "Base",
				"relative_path": "/repo/" + id + ".rb",
			},
		}
	}
	child := facts.Envelope{
		FactKind: "content_entity",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       "repo-1",
			"entity_id":     "content-entity:e_kid",
			"entity_type":   "Class",
			"entity_name":   "Kid",
			"relative_path": "/repo/kid.rb",
			"entity_metadata": map[string]any{
				"bases": []any{"Base"},
			},
		},
	}

	orders := [][]facts.Envelope{
		{parent("content-entity:e_base_1"), parent("content-entity:e_base_2"), parent("content-entity:e_base_3"), child},
		{parent("content-entity:e_base_3"), parent("content-entity:e_base_2"), parent("content-entity:e_base_1"), child},
		{child, parent("content-entity:e_base_2"), parent("content-entity:e_base_3"), parent("content-entity:e_base_1")},
	}
	var first []map[string]any
	for i, envelopes := range orders {
		_, rows := ExtractRows(envelopes)
		if len(rows) != 1 {
			t.Fatalf("order %d: len(rows) = %d, want 1", i, len(rows))
		}
		if got := rows[0]["parent_entity_id"]; got != "content-entity:e_base_1" {
			t.Fatalf("order %d: parent_entity_id = %#v, want the smallest entity_id", i, got)
		}
		if i == 0 {
			first = rows
			continue
		}
		if !reflect.DeepEqual(rows, first) {
			t.Fatalf("order %d: rows = %#v, want %#v (order-independent)", i, rows, first)
		}
	}
}

// --- test doubles ---

func inheritanceEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: factload.FactKindRepository,
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
				// "relative_path" is the key gitcontent.ContentEntityFactEnvelope actually
				// emits (go/internal/collector/git/content/envelopes.go);
				// production carries no
				// top-level "path" key (#5996).
				"relative_path": "/repo/parent.py",
			},
		},
		{
			FactKind: "content_entity",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_child",
				"entity_type":   "Class",
				"entity_name":   "ChildClass",
				"relative_path": "/repo/child.py",
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}
}
