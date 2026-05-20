package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentationHandlerPassesPersistedScopeFilters(t *testing.T) {
	t.Parallel()

	var captured documentationFindingFilter
	handler := &DocumentationHandler{
		Content: fakePortContentStore{documentationFindingsFilter: &captured},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/findings?scope_id=docs-scope&generation_id=gen-1&repo=acme/api",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.ScopeID, "docs-scope"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := captured.GenerationID, "gen-1"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := captured.Repository, "acme/api"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
}

func TestBuildDocumentationFindingsSQLFiltersPersistedScopeIdentity(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationFindingsSQL(documentationFindingFilter{
		ScopeID:      "docs-scope",
		GenerationID: "gen-1",
		Repository:   "acme/api",
		Limit:        50,
	})

	for _, fragment := range []string{
		"jsonb_build_object('scope_id', fact_records.scope_id, 'generation_id', fact_records.generation_id)",
		"LEFT JOIN ingestion_scopes",
		"fact_records.scope_id = $1",
		"fact_records.generation_id = $2",
		"ingestion_scopes.metadata->>'repo' = $3",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("documentation findings SQL missing fragment %q: %s", fragment, query)
		}
	}
	if got, want := args[:3], []any{"docs-scope", "gen-1", "acme/api"}; !equalDocumentationAnySlice(got, want) {
		t.Fatalf("filter args = %#v, want %#v", got, want)
	}
}

func TestDocumentationHandlerListsScopeMetadataInFindings(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFindingsModel: documentationFindingListReadModel{
				Findings: []map[string]any{{
					"finding_id":    "finding:docs:1",
					"scope_id":      "docs-scope",
					"generation_id": "gen-1",
					"repo":          "acme/api",
				}},
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings?scope_id=docs-scope", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	findings := resp["findings"].([]any)
	finding := findings[0].(map[string]any)
	if got, want := finding["scope_id"], "docs-scope"; got != want {
		t.Fatalf("scope_id = %#v, want %#v", got, want)
	}
	if got, want := finding["generation_id"], "gen-1"; got != want {
		t.Fatalf("generation_id = %#v, want %#v", got, want)
	}
	if got, want := finding["repo"], "acme/api"; got != want {
		t.Fatalf("repo = %#v, want %#v", got, want)
	}
}

func equalDocumentationAnySlice(got []any, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
