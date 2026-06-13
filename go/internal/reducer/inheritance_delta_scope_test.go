package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestInheritanceMaterializationHandlerScopesDeltaRetractToFiles(t *testing.T) {
	t.Parallel()

	writer := &recordingInheritanceEdgeWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: inheritanceDeltaEntityFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-inheritance-delta",
		ScopeID:      "scope-code",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 9, 5, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 9, 5, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.retractDomain != DomainInheritanceEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainInheritanceEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	payload := writer.retractRows[0].Payload
	if got, ok := payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("delta_projection = %#v, want true", payload["delta_projection"])
	}
	gotPaths, ok := payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", payload["delta_file_paths"])
	}
	wantPaths := []string{"/repo/src/child.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if len(writer.writeRows) != 1 {
		t.Fatalf("writeRows len = %d, want 1", len(writer.writeRows))
	}
}

func TestInheritanceMaterializationHandlerDeletedOnlyDeltaRetractsWithoutWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingInheritanceEdgeWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":                      "repo-123",
					"local_path":                   "/repo",
					"delta_generation":             true,
					"delta_deleted_relative_paths": []string{"src/deleted.go"},
				},
			},
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-inheritance-deleted",
		ScopeID:      "scope-code",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 9, 10, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 9, 10, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if writer.retractDomain != DomainInheritanceEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainInheritanceEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	gotPaths, ok := writer.retractRows[0].Payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", writer.retractRows[0].Payload["delta_file_paths"])
	}
	wantPaths := []string{"/repo/src/deleted.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows len = %d, want 0", len(writer.writeRows))
	}
}

func TestBuildInheritanceRetractRowsKeepsMalformedDeltaScoped(t *testing.T) {
	t.Parallel()

	rows := buildInheritanceRetractRows([]string{"repo-123"}, inheritanceDeltaScope{
		repositoryIDs: []string{"repo-123"},
		hasDelta:      true,
	})
	if len(rows) != 1 {
		t.Fatalf("retract rows len = %d, want 1", len(rows))
	}
	payload := rows[0].Payload
	if got, ok := payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("delta_projection = %#v, want true", payload["delta_projection"])
	}
	if gotPaths := semanticPayloadStringSlice(payload, "delta_file_paths"); len(gotPaths) != 0 {
		t.Fatalf("delta_file_paths = %#v, want empty malformed delta scope", gotPaths)
	}
}

func inheritanceDeltaEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":                      "repo-123",
				"local_path":                   "/repo",
				"delta_generation":             true,
				"delta_relative_paths":         []string{"src/child.go", "../outside.go"},
				"delta_deleted_relative_paths": []string{},
			},
		},
		{
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_parent",
				"entity_type": "Class",
				"entity_name": "ParentClass",
			},
		},
		{
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repo_id":     "repo-123",
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
