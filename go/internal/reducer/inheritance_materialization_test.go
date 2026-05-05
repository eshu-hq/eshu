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
		FactLoader: &stubFactLoader{},
		EdgeWriter: &recordingInheritanceEdgeWriter{},
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
		EdgeWriter: &recordingInheritanceEdgeWriter{},
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

func TestInheritanceMaterializationHandlerRequiresEdgeWriter(t *testing.T) {
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
		t.Fatal("expected error for nil edge writer, got nil")
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

func TestInheritanceMaterializationHandlerWritesEdges(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	writer := &recordingInheritanceEdgeWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
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
			},
		},
		EdgeWriter: writer,
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
	if result.CanonicalWrites != 1 {
		t.Fatalf("result.CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}

	if writer.retractDomain != DomainInheritanceEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainInheritanceEdges)
	}
	if writer.retractEvidenceSource != inheritanceEvidenceSource {
		t.Fatalf("retractEvidenceSource = %q, want %q", writer.retractEvidenceSource, inheritanceEvidenceSource)
	}
	if writer.writeDomain != DomainInheritanceEdges {
		t.Fatalf("writeDomain = %q, want %q", writer.writeDomain, DomainInheritanceEdges)
	}
	if writer.writeEvidenceSource != inheritanceEvidenceSource {
		t.Fatalf("writeEvidenceSource = %q, want %q", writer.writeEvidenceSource, inheritanceEvidenceSource)
	}
	if len(writer.writeRows) != 1 {
		t.Fatalf("len(writeRows) = %d, want 1", len(writer.writeRows))
	}
	if got, want := writer.writeRows[0].Payload["child_entity_id"], "content-entity:e_child"; got != want {
		t.Fatalf("child_entity_id = %#v, want %#v", got, want)
	}
	if got, want := writer.writeRows[0].Payload["parent_entity_id"], "content-entity:e_parent"; got != want {
		t.Fatalf("parent_entity_id = %#v, want %#v", got, want)
	}
}

func TestInheritanceMaterializationHandlerSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := &recordingInheritanceEdgeWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: inheritanceEntityFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(_ context.Context, scopeID, generationID string) (bool, error) {
			if scopeID != "scope-1" || generationID != "gen-1" {
				t.Fatalf("PriorGenerationCheck(%q, %q), want scope-1/gen-1", scopeID, generationID)
			}
			return false, nil
		},
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-inheritance-first",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("result.CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.retractDomain != "" || len(writer.retractRows) != 0 {
		t.Fatalf("retract = domain %q rows %d, want skipped", writer.retractDomain, len(writer.retractRows))
	}
	if len(writer.writeRows) != 1 {
		t.Fatalf("len(writeRows) = %d, want 1", len(writer.writeRows))
	}
}

func TestInheritanceMaterializationHandlerRetractsWhenPriorGenerationExists(t *testing.T) {
	t.Parallel()

	writer := &recordingInheritanceEdgeWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: inheritanceEntityFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-inheritance-prior",
		ScopeID:      "scope-1",
		GenerationID: "gen-2",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if writer.retractDomain != DomainInheritanceEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainInheritanceEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("len(retractRows) = %d, want 1", len(writer.retractRows))
	}
}

func TestInheritanceMaterializationHandlerRetractsOnRetryEvenForFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := &recordingInheritanceEdgeWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: inheritanceEntityFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return false, nil
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-inheritance-retry",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainInheritanceMaterialization,
		AttemptCount: 2,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if writer.retractDomain != DomainInheritanceEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainInheritanceEdges)
	}
}

func TestInheritanceMaterializationHandlerNoEntitiesSucceeds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	writer := &recordingInheritanceEdgeWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{}},
		EdgeWriter: writer,
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
}

// --- test doubles ---

func inheritanceEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_parent",
				"entity_type": "Class",
				"entity_name": "ParentClass",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_child",
				"entity_type": "Class",
				"entity_name": "ChildClass",
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}
}

type recordingInheritanceEdgeWriter struct {
	retractDomain         string
	retractEvidenceSource string
	retractRows           []SharedProjectionIntentRow
	writeDomain           string
	writeEvidenceSource   string
	writeRows             []SharedProjectionIntentRow
}

func (r *recordingInheritanceEdgeWriter) RetractEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	r.retractDomain = domain
	r.retractEvidenceSource = evidenceSource
	r.retractRows = append(r.retractRows, rows...)
	return nil
}

func (r *recordingInheritanceEdgeWriter) WriteEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	r.writeDomain = domain
	r.writeEvidenceSource = evidenceSource
	r.writeRows = append(r.writeRows, rows...)
	return nil
}
