// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSQLRelationshipRowsFromEmbeddedQueryOnKnownTable(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipContentEntity("content-entity:users", "SqlTable", "public.users", "db/schema.sql", nil),
		sqlRelationshipFileWithEmbeddedQuery("public.users"),
	}

	repoIDs, rows := ExtractSQLRelationshipRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%+v", len(rows), rows)
	}
	row := rows[0]
	if got, want := row["source_entity_id"], "content-entity:handle"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := row["target_entity_id"], "content-entity:users"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := row["source_entity_type"], "Function"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := row["target_entity_type"], "SqlTable"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
	if got, want := row["source_path"], "/repo/cmd/api/handlers.go"; got != want {
		t.Fatalf("source_path = %v, want %v", got, want)
	}
	if got, want := row["relationship_type"], "QUERIES_TABLE"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
}

func TestExtractSQLRelationshipRowsSkipsAmbiguousEmbeddedQueryTable(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipContentEntity("content-entity:users-a", "SqlTable", "public.users", "db/schema_a.sql", nil),
		sqlRelationshipContentEntity("content-entity:users-b", "SqlTable", "public.users", "db/schema_b.sql", nil),
		sqlRelationshipFileWithEmbeddedQuery("public.users"),
	}

	_, rows := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for ambiguous table reference; rows=%+v", len(rows), rows)
	}
}

func TestExtractSQLRelationshipRowsSkipsEmbeddedQueryWithoutSourcePath(t *testing.T) {
	t.Parallel()

	fileFact := sqlRelationshipFileWithEmbeddedQuery("public.users")
	delete(fileFact.Payload, "path")
	envelopes := []facts.Envelope{
		sqlRelationshipContentEntity("content-entity:users", "SqlTable", "public.users", "db/schema.sql", nil),
		fileFact,
	}

	_, rows := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 without repo-qualified source path; rows=%+v", len(rows), rows)
	}
}

func TestExtractSQLRelationshipRowsReadsEmbeddedQueryPathFromParsedFileData(t *testing.T) {
	t.Parallel()

	fileFact := sqlRelationshipFileWithEmbeddedQuery("public.users")
	delete(fileFact.Payload, "path")
	parsedFileData := fileFact.Payload["parsed_file_data"].(map[string]any)
	parsedFileData["path"] = "/repo/cmd/api/handlers.go"
	envelopes := []facts.Envelope{
		sqlRelationshipContentEntity("content-entity:users", "SqlTable", "public.users", "db/schema.sql", nil),
		fileFact,
	}

	_, rows := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 from parsed_file_data.path; rows=%+v", len(rows), rows)
	}
	if got, want := rows[0]["source_path"], "/repo/cmd/api/handlers.go"; got != want {
		t.Fatalf("source_path = %v, want %v", got, want)
	}
}

func TestSQLRelationshipHandlerEmitsEmbeddedQueryIntent(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelationshipIntentWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			sqlRelationshipRepositoryEnvelope(false, nil),
			sqlRelationshipContentEntity("content-entity:users", "SqlTable", "public.users", "db/schema.sql", nil),
			sqlRelationshipFileWithEmbeddedQuery("public.users"),
		}},
		IntentWriter: writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-query-1",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	edges := writer.edgeRows()
	if len(edges) != 1 {
		t.Fatalf("edge intents = %d, want 1; rows=%+v", len(edges), writer.rows)
	}
	if got, want := edges[0].Payload["relationship_type"], "QUERIES_TABLE"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := edges[0].Payload["source_entity_id"], "content-entity:handle"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := edges[0].Payload["target_entity_id"], "content-entity:users"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if !rowUsesRefreshFence(edges[0]) {
		t.Fatalf("embedded query intent %q is not fenced behind the refresh", edges[0].IntentID)
	}
}

func TestLoadSQLRelationshipMaterializationFactsIncludesFileFacts(t *testing.T) {
	t.Parallel()

	loader := &recordingSQLRelationshipKindPayloadLoader{
		byKind: map[string][]facts.Envelope{
			factKindRepository: {sqlRelationshipRepositoryEnvelope(false, nil)},
			factKindFile:       {sqlRelationshipFileWithEmbeddedQuery("public.users")},
		},
		byPayload: []facts.Envelope{
			sqlRelationshipContentEntity("content-entity:users", "SqlTable", "public.users", "db/schema.sql", nil),
		},
	}

	envelopes, err := loadSQLRelationshipMaterializationFacts(context.Background(), loader, "scope-db", "gen-1")
	if err != nil {
		t.Fatalf("loadSQLRelationshipMaterializationFacts() error = %v", err)
	}
	if !sqlRelationshipEnvelopeKinds(envelopes)[factKindFile] {
		t.Fatalf("loaded fact kinds = %v, want file facts included", sqlRelationshipEnvelopeKinds(envelopes))
	}
	if got, want := sqlRelationshipKindCalls(loader.kindCalls), "repository;file"; got != want {
		t.Fatalf("kind calls = %q, want %q", got, want)
	}
}

func sqlRelationshipFileWithEmbeddedQuery(tableName string) facts.Envelope {
	return facts.Envelope{
		FactKind: factKindFile,
		ScopeID:  "scope-db",
		Payload: map[string]any{
			"repo_id":       "repo-123",
			"relative_path": "cmd/api/handlers.go",
			"path":          "/repo/cmd/api/handlers.go",
			"parsed_file_data": map[string]any{
				"functions": []any{
					map[string]any{
						"name":        "handle",
						"uid":         "content-entity:handle",
						"line_number": 10,
						"end_line":    20,
					},
				},
				"embedded_sql_queries": []any{
					map[string]any{
						"function_name":        "handle",
						"function_line_number": 10,
						"table_name":           tableName,
						"operation":            "select",
						"line_number":          13,
						"api":                  "database/sql",
					},
				},
			},
		},
	}
}

type recordingSQLRelationshipKindPayloadLoader struct {
	byKind      map[string][]facts.Envelope
	byPayload   []facts.Envelope
	kindCalls   [][]string
	payloadCall bool
}

func (l *recordingSQLRelationshipKindPayloadLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return nil, nil
}

func (l *recordingSQLRelationshipKindPayloadLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	factKinds []string,
) ([]facts.Envelope, error) {
	l.kindCalls = append(l.kindCalls, append([]string(nil), factKinds...))
	var out []facts.Envelope
	for _, kind := range factKinds {
		out = append(out, l.byKind[kind]...)
	}
	return out, nil
}

func (l *recordingSQLRelationshipKindPayloadLoader) ListFactsByKindAndPayloadValue(
	context.Context,
	string,
	string,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	l.payloadCall = true
	return append([]facts.Envelope(nil), l.byPayload...), nil
}

func sqlRelationshipEnvelopeKinds(envelopes []facts.Envelope) map[string]bool {
	out := make(map[string]bool)
	for _, envelope := range envelopes {
		out[envelope.FactKind] = true
	}
	return out
}

func sqlRelationshipKindCalls(calls [][]string) string {
	parts := make([]string, 0, len(calls))
	for _, call := range calls {
		parts = append(parts, strings.Join(call, ","))
	}
	return strings.Join(parts, ";")
}
