// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	writer := &recordingInheritanceIntentWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: inheritanceDeltaEntityFacts()},
		IntentWriter: writer,
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
	refresh := writer.refreshRows()
	if len(refresh) != 1 {
		t.Fatalf("refresh intents = %d, want 1", len(refresh))
	}
	payload := refresh[0].Payload
	if got, ok := payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("delta_projection = %#v, want true", payload["delta_projection"])
	}
	gotPaths, ok := payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", payload["delta_file_paths"])
	}
	if !reflect.DeepEqual(gotPaths, []string{"/repo/src/child.go"}) {
		t.Fatalf("delta_file_paths = %#v, want [/repo/src/child.go]", gotPaths)
	}
	// The changed file's edge still projects.
	if len(writer.edgeRows()) != 1 {
		t.Fatalf("per-edge intents = %d, want 1", len(writer.edgeRows()))
	}
}

func TestInheritanceMaterializationHandlerDeletedOnlyDeltaRetractsWithoutWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingInheritanceIntentWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				ScopeID:  "scope-code",
				Payload: map[string]any{
					"repo_id":                      "repo-123",
					"path":                         "/repo",
					"local_path":                   "/repo",
					"source_run_id":                "run-1",
					"delta_generation":             true,
					"delta_deleted_relative_paths": []string{"src/deleted.go"},
				},
			},
		}},
		IntentWriter: writer,
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
	// Only the refresh intent (it owns the file-scoped retract); no per-edge
	// writes because the only changed file was deleted.
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	refresh := writer.refreshRows()
	if len(refresh) != 1 {
		t.Fatalf("refresh intents = %d, want 1", len(refresh))
	}
	gotPaths, ok := refresh[0].Payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", refresh[0].Payload["delta_file_paths"])
	}
	if !reflect.DeepEqual(gotPaths, []string{"/repo/src/deleted.go"}) {
		t.Fatalf("delta_file_paths = %#v, want [/repo/src/deleted.go]", gotPaths)
	}
	if len(writer.edgeRows()) != 0 {
		t.Fatalf("per-edge intents = %d, want 0", len(writer.edgeRows()))
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
			ScopeID:  "scope-code",
			Payload: map[string]any{
				"repo_id":                      "repo-123",
				"path":                         "/repo",
				"local_path":                   "/repo",
				"source_run_id":                "run-1",
				"delta_generation":             true,
				"delta_relative_paths":         []string{"src/child.go", "../outside.go"},
				"delta_deleted_relative_paths": []string{},
			},
		},
		{
			FactKind: factKindContentEntity,
			ScopeID:  "scope-code",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_parent",
				"entity_type": "Class",
				"entity_name": "ParentClass",
				"path":        "/repo/src/parent.go",
			},
		},
		{
			FactKind: factKindContentEntity,
			ScopeID:  "scope-code",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_child",
				"entity_type": "Class",
				"entity_name": "ChildClass",
				"path":        "/repo/src/child.go",
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}
}
