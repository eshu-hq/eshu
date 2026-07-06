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

func TestSQLRelationshipMaterializationHandlerScopesDeltaRetractToFiles(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelationshipIntentWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: sqlRelationshipDeltaEntityFacts()},
		IntentWriter: writer,
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
	// The per-repo refresh intent owns the file-scoped retract and carries the
	// changed files' repo-qualified paths.
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
	wantPaths := []string{"/repo/db/schema.sql"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	// The changed file's REFERENCES_TABLE edge still projects.
	if len(writer.edgeRows()) != 1 {
		t.Fatalf("per-edge intents = %d, want 1", len(writer.edgeRows()))
	}
}

func TestSQLRelationshipMaterializationHandlerDeletedOnlyDeltaRetractsWithoutWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelationshipIntentWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				ScopeID:  "scope-db",
				Payload: map[string]any{
					"repo_id":                        "repo-123",
					"path":                           "/repo",
					"local_path":                     "/repo",
					"source_run_id":                  "run-1",
					"delta_generation":               true,
					"delta_deleted_relative_paths":   []string{"db/deleted.sql"},
					"delta_relative_paths":           []string{"db/deleted.sql"},
					"unrelated_delta_relative_paths": []string{"db/ignored.sql"},
				},
			},
		}},
		IntentWriter: writer,
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
	// Only the refresh intent (it owns the file-scoped retract); no per-edge
	// writes because the only changed file was deleted (no surviving entities).
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
	wantPaths := []string{"/repo/db/deleted.sql"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if len(writer.edgeRows()) != 0 {
		t.Fatalf("per-edge intents = %d, want 0", len(writer.edgeRows()))
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

// TestBuildSQLRelationshipDeltaScopeSkipsWhitespaceOnlyRelativePath is the
// TDD regression for the Wave 4f S2 (issue #4754) typed-decode conversion of
// buildSQLRelationshipDeltaScope: before this migration, the delta path
// collection ran through semanticPayloadStringSlice, which trims each
// delta_relative_paths entry and drops it if the trimmed result is empty,
// so a whitespace-only entry never reached qualifySQLRelationshipDeltaFilePath.
// The migration reuses codeCallDeltaRelativePathsFromRepository (the codegraph
// decode seam's typed delta-path union), which returns each JSON array
// element RAW with no trimming — so a whitespace-only entry that would have
// been silently dropped can leak through path.Clean (which does not trim
// whitespace) into a bogus "<repoPath>/  "-shaped qualified path if the
// conversion does not add its own trim-and-skip step. This test proves the
// conversion added that step: a whitespace-only entry is dropped, and a
// genuine entry alongside it still qualifies normally.
func TestBuildSQLRelationshipDeltaScopeSkipsWhitespaceOnlyRelativePath(t *testing.T) {
	t.Parallel()

	env := facts.Envelope{
		FactKind: factKindRepository,
		Payload: map[string]any{
			"repo_id":              "repo-whitespace",
			"local_path":           "/repo",
			"delta_generation":     true,
			"delta_relative_paths": []any{"   ", "db/real.sql"},
		},
	}

	scope := buildSQLRelationshipDeltaScope([]facts.Envelope{env})

	gotPaths := scope.filePathsByRepoID["repo-whitespace"]
	wantPaths := []string{"/repo/db/real.sql"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("filePathsByRepoID[%q] = %#v, want %#v (whitespace-only entry must be dropped, not qualified into a bogus path)", "repo-whitespace", gotPaths, wantPaths)
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
			ScopeID:  "scope-db",
			Payload: map[string]any{
				"repo_id":                        "repo-123",
				"path":                           "/repo",
				"local_path":                     "/repo",
				"source_run_id":                  "run-1",
				"delta_generation":               true,
				"delta_relative_paths":           []string{"db/schema.sql", "../outside.sql"},
				"delta_deleted_relative_paths":   []string{},
				"unrelated_delta_relative_paths": []string{"db/ignored.sql"},
			},
		},
		{
			FactKind: "content_entity",
			ScopeID:  "scope-db",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_tbl1",
				"entity_type":   "SqlTable",
				"entity_name":   "public.users",
				"relative_path": "db/schema.sql",
				"path":          "/repo/db/schema.sql",
			},
		},
		{
			FactKind: "content_entity",
			ScopeID:  "scope-db",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_view1",
				"entity_type":   "SqlView",
				"entity_name":   "public.active_users",
				"relative_path": "db/schema.sql",
				"path":          "/repo/db/schema.sql",
				"entity_metadata": map[string]any{
					"source_tables": []any{"public.users"},
				},
			},
		},
	}
}
