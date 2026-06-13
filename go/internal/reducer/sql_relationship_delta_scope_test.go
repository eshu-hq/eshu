package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSQLRelationshipMaterializationHandlerScopesDeltaRetractToFiles(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelEdgeWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: sqlRelationshipDeltaEntityFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-rel-delta",
		ScopeID:      "scope-db",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 8, 30, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 8, 30, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.retractDomain != DomainSQLRelationships {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainSQLRelationships)
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
	wantPaths := []string{"/repo/db/schema.sql"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if len(writer.writeRows) != 1 {
		t.Fatalf("writeRows len = %d, want 1", len(writer.writeRows))
	}
}

func TestSQLRelationshipMaterializationHandlerDeletedOnlyDeltaRetractsWithoutWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelEdgeWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":                        "repo-123",
					"local_path":                     "/repo",
					"delta_generation":               true,
					"delta_deleted_relative_paths":   []string{"db/deleted.sql"},
					"delta_relative_paths":           []string{"db/deleted.sql"},
					"unrelated_delta_relative_paths": []string{"db/ignored.sql"},
				},
			},
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-rel-deleted",
		ScopeID:      "scope-db",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 8, 35, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 8, 35, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if writer.retractDomain != DomainSQLRelationships {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainSQLRelationships)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	gotPaths, ok := writer.retractRows[0].Payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", writer.retractRows[0].Payload["delta_file_paths"])
	}
	wantPaths := []string{"/repo/db/deleted.sql"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows len = %d, want 0", len(writer.writeRows))
	}
}

func TestBuildSQLRelationshipRetractRowsKeepsMalformedDeltaScoped(t *testing.T) {
	t.Parallel()

	rows := buildSQLRelationshipRetractRows([]string{"repo-123"}, sqlRelationshipDeltaScope{
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

func TestLoadSQLRelationshipMaterializationFactsUsesSingleLegacyFallback(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{envelopes: sqlRelationshipDeltaEntityFacts()}
	envelopes, err := loadSQLRelationshipMaterializationFacts(context.Background(), loader, "scope-db", "gen-2")
	if err != nil {
		t.Fatalf("loadSQLRelationshipMaterializationFacts() error = %v, want nil", err)
	}
	if loader.calls != 1 {
		t.Fatalf("ListFacts() calls = %d, want 1 fallback load", loader.calls)
	}
	if len(envelopes) != len(sqlRelationshipDeltaEntityFacts()) {
		t.Fatalf("envelopes len = %d, want %d", len(envelopes), len(sqlRelationshipDeltaEntityFacts()))
	}
}

func sqlRelationshipDeltaEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":                        "repo-123",
				"local_path":                     "/repo",
				"delta_generation":               true,
				"delta_relative_paths":           []string{"db/schema.sql", "../outside.sql"},
				"delta_deleted_relative_paths":   []string{},
				"unrelated_delta_relative_paths": []string{"db/ignored.sql"},
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_tbl1",
				"entity_type":   "SqlTable",
				"entity_name":   "public.users",
				"relative_path": "db/schema.sql",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_view1",
				"entity_type":   "SqlView",
				"entity_name":   "public.active_users",
				"relative_path": "db/schema.sql",
				"entity_metadata": map[string]any{
					"source_tables": []any{"public.users"},
				},
			},
		},
	}
}
