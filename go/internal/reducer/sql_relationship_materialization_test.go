// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSQLRelationshipHandlerRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := SQLRelationshipMaterializationHandler{
		FactLoader:   &stubFactLoader{},
		IntentWriter: &recordingSQLRelationshipIntentWriter{},
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
		t.Fatal("expected error for mismatched domain")
	}
}

func TestSQLRelationshipHandlerRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := SQLRelationshipMaterializationHandler{
		IntentWriter: &recordingSQLRelationshipIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestSQLRelationshipHandlerRequiresIntentWriter(t *testing.T) {
	t.Parallel()

	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when intent writer is nil")
	}
}

func TestExtractSQLRelationshipRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	repoIDs, rows, _ := ExtractSQLRelationshipRows(nil)
	if repoIDs != nil {
		t.Fatalf("repoIDs = %v, want nil", repoIDs)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
}

func TestExtractSQLRelationshipRowsNoRelationshipsReturnsEmpty(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
	}

	repoIDs, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestExtractSQLRelationshipRowsFromTableWithColumn(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_col1",
				"entity_type": "SqlColumn",
				"entity_name": "public.users.email",
				"entity_metadata": map[string]any{
					"table_name":      "public.users",
					"sql_entity_type": "SqlColumn",
				},
			},
		},
	}

	repoIDs, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["source_entity_id"], "content-entity:e_tbl1"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_col1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "HAS_COLUMN"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["source_entity_type"], "SqlTable"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlColumn"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
}

func TestSQLRelationshipHandlerEmitsIntents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	writer := &recordingSQLRelationshipIntentWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				sqlRelationshipRepositoryEnvelope(false, nil),
				sqlRelationshipContentEntity("content-entity:e_tbl1", "SqlTable", "public.users", "db/schema.sql", nil),
				sqlRelationshipContentEntity("content-entity:e_view1", "SqlView", "public.active_users", "db/schema.sql", map[string]any{
					"source_tables":   []any{"public.users"},
					"sql_entity_type": "SqlView",
				}),
				sqlRelationshipContentEntity("content-entity:e_col1", "SqlColumn", "public.users.email", "db/schema.sql", map[string]any{
					"table_name":      "public.users",
					"sql_entity_type": "SqlColumn",
				}),
				sqlRelationshipContentEntity("content-entity:e_trig1", "SqlTrigger", "users_touch", "db/schema.sql", map[string]any{
					"table_name":      "public.users",
					"sql_entity_type": "SqlTrigger",
				}),
			},
		},
		IntentWriter: writer,
	}

	intent := Intent{
		IntentID:        "intent-sql-rel-1",
		ScopeID:         "scope-db",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainSQLRelationshipMaterialization,
		Cause:           "sql relationship follow-up required",
		EntityKeys:      []string{"repo-123"},
		RelatedScopeIDs: []string{"scope-db"},
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
	// One per-repo refresh intent (owns the retract) plus three per-edge intents
	// (HAS_COLUMN, READS_FROM, TRIGGERS).
	if result.CanonicalWrites != 4 {
		t.Fatalf("result.CanonicalWrites = %d, want 4", result.CanonicalWrites)
	}

	refresh := writer.refreshRows()
	if len(refresh) != 1 {
		t.Fatalf("refresh intents = %d, want 1", len(refresh))
	}
	if got := refresh[0].ProjectionDomain; got != DomainSQLRelationships {
		t.Fatalf("refresh domain = %q, want %q", got, DomainSQLRelationships)
	}
	if refresh[0].PartitionKey != sqlRelationshipWholeScopePartitionKey("repo-123") {
		t.Fatalf("refresh partition key = %q, want whole-scope key", refresh[0].PartitionKey)
	}

	edges := writer.edgeRows()
	if len(edges) != 3 {
		t.Fatalf("per-edge intents = %d, want 3", len(edges))
	}
	gotTypes := make([]string, 0, len(edges))
	for _, edge := range edges {
		if edge.ProjectionDomain != DomainSQLRelationships {
			t.Fatalf("edge domain = %q, want %q", edge.ProjectionDomain, DomainSQLRelationships)
		}
		if !rowUsesRefreshFence(edge) {
			t.Fatalf("edge intent %q not marked retract_via_refresh", edge.IntentID)
		}
		if !strings.HasPrefix(edge.PartitionKey, sqlRelationshipPartitionKeyVersion+":files:") {
			t.Fatalf("edge partition key %q lacks file-scoped prefix", edge.PartitionKey)
		}
		gotTypes = append(gotTypes, anyToString(edge.Payload["relationship_type"]))
	}
	sort.Strings(gotTypes)
	if want := []string{"HAS_COLUMN", "READS_FROM", "TRIGGERS"}; strings.Join(gotTypes, ",") != strings.Join(want, ",") {
		t.Fatalf("edge relationship types = %v, want %v", gotTypes, want)
	}
}

func TestSQLRelationshipHandlerLogsCompletion(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	handler := SQLRelationshipMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: sqlRelationshipEntityFacts()},
		IntentWriter: &recordingSQLRelationshipIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-rel-1",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"sql relationship materialization completed"`,
		`"edge_count":1`,
		`"repo_count":1`,
		`"intent_count":2`,
		`"unresolved_read_targets":0`,
		`"ambiguous_read_targets":0`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}

func TestSQLRelationshipHandlerSkipsWhenNoProjectionContext(t *testing.T) {
	t.Parallel()

	// Content entities with no repository envelope -> no projection context, so
	// the handler emits nothing rather than fabricating an unfenceable edge (the
	// refresh no-op-retracts safely on a first generation; there is no first-gen
	// skip to assert anymore).
	writer := &recordingSQLRelationshipIntentWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			sqlRelationshipContentEntity("content-entity:e_tbl1", "SqlTable", "public.users", "db/schema.sql", nil),
			sqlRelationshipContentEntity("content-entity:e_view1", "SqlView", "public.active_users", "db/schema.sql", map[string]any{
				"source_tables": []any{"public.users"},
			}),
		}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-rel-1",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("emitted %d intents, want 0 without a projection context", len(writer.rows))
	}
}

func TestSQLRelationshipHandlerEmitsRefreshThatOwnsRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelationshipIntentWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: sqlRelationshipEntityFacts()},
		IntentWriter: writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-rel-1",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	refresh := writer.refreshRows()
	if len(refresh) != 1 {
		t.Fatalf("refresh intents = %d, want 1", len(refresh))
	}
	if got := anyToString(refresh[0].Payload["intent_type"]); got != repoRefreshIntentType {
		t.Fatalf("refresh intent_type = %q, want %q", got, repoRefreshIntentType)
	}
	if got := anyToString(refresh[0].Payload["action"]); got != repoRefreshAction {
		t.Fatalf("refresh action = %q, want %q", got, repoRefreshAction)
	}
}

// --- test stubs ---

func sqlRelationshipRepositoryEnvelope(delta bool, changedRelPaths []string) facts.Envelope {
	payload := map[string]any{
		"repo_id":       "repo-123",
		"path":          "/repo",
		"source_run_id": "run-1",
	}
	if delta {
		payload["delta_generation"] = true
		payload["delta_relative_paths"] = changedRelPaths
	}
	return facts.Envelope{FactKind: factKindRepository, ScopeID: "scope-db", Payload: payload}
}

func sqlRelationshipContentEntity(id, entityType, name, relPath string, metadata map[string]any) facts.Envelope {
	payload := map[string]any{
		"repo_id":       "repo-123",
		"entity_id":     id,
		"entity_type":   entityType,
		"entity_name":   name,
		"relative_path": relPath,
		"path":          "/repo/" + relPath,
	}
	if metadata != nil {
		payload["entity_metadata"] = metadata
	}
	return facts.Envelope{FactKind: factKindContentEntity, ScopeID: "scope-db", Payload: payload}
}

func sqlRelationshipEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		sqlRelationshipRepositoryEnvelope(false, nil),
		sqlRelationshipContentEntity("content-entity:e_tbl1", "SqlTable", "public.users", "db/schema.sql", nil),
		sqlRelationshipContentEntity("content-entity:e_view1", "SqlView", "public.active_users", "db/schema.sql", map[string]any{
			"source_tables": []any{"public.users"},
		}),
	}
}
