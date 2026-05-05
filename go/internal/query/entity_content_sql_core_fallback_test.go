package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveEntityFallsBackToSQLFunctionContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"sql-function-1", "repo-1", "schema.sql", "SqlFunction", "public.archive_audit",
					int64(9), int64(18), "sql", "", []byte(`{"routine_kind":"procedure","function_language":"plpgsql"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"public.archive_audit","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one content-backed entity", resp["entities"])
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}
	metadata, ok := entity["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("entity[metadata] type = %T, want map[string]any", entity["metadata"])
	}
	if got, want := metadata["routine_kind"], "procedure"; got != want {
		t.Fatalf("entity[metadata][routine_kind] = %#v, want %#v", got, want)
	}
	if got, want := metadata["function_language"], "plpgsql"; got != want {
		t.Fatalf("entity[metadata][function_language] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToSQLTableContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"sql-table-1", "repo-1", "schema.sql", "SqlTable", "public.audit_logs",
					int64(1), int64(4), "sql", "", []byte(`{"schema":"public","qualified_name":"public.audit_logs"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/sql-table-1/context", nil)
	req.SetPathValue("entity_id", "sql-table-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["name"], "public.audit_logs"; got != want {
		t.Fatalf("resp[name] = %#v, want %#v", got, want)
	}
	metadata, ok := resp["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("resp[metadata] type = %T, want map[string]any", resp["metadata"])
	}
	if got, want := metadata["qualified_name"], "public.audit_logs"; got != want {
		t.Fatalf("resp[metadata][qualified_name] = %#v, want %#v", got, want)
	}
}
