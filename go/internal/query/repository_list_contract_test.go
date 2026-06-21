package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRepositoriesReturnsBoundedEnvelopeFromContentCatalog(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repository:one", Name: "one"},
				{ID: "repository:two", Name: "two"},
				{ID: "repository:three", Name: "three"},
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=2", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	repositories, ok := data["repositories"].([]any)
	if !ok || len(repositories) != 2 {
		t.Fatalf("repositories = %#v, want two rows", data["repositories"])
	}
	if got, want := data["limit"], float64(2); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != "platform_impact.context_overview" {
		t.Fatalf("truth = %#v, want context overview truth", envelope.Truth)
	}
}

// TestListRepositoriesTotalIsIndependentOfPageSize asserts that the response
// carries a "total" field reflecting the full repository count, not the page
// slice length. This is the regression guard for issue #3392: previously the
// only count in the response was the page-slice length, so a limit=1 request
// always returned count=1 instead of the true total.
func TestListRepositoriesTotalIsIndependentOfPageSize(t *testing.T) {
	t.Parallel()

	// Content store has three repositories; request only one per page.
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repository:one", Name: "one"},
				{ID: "repository:two", Name: "two"},
				{ID: "repository:three", Name: "three"},
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=1&offset=0", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	// count reflects the page slice (1 item returned).
	if got, want := body["count"], float64(1); got != want {
		t.Fatalf("count = %#v, want %#v (page slice length)", got, want)
	}
	// total must reflect all three repositories regardless of page size.
	if got, want := body["total"], float64(3); got != want {
		t.Fatalf("total = %#v, want %#v (true repository count independent of page)", got, want)
	}
}

// TestListRepositoriesTotalFromGraphIsIndependentOfPageSize asserts that when
// the graph backend is present the response total reflects a true COUNT query,
// not the page slice. The fake graph returns two rows for the page query and a
// separate count row for the total query.
func TestListRepositoriesTotalFromGraphIsIndependentOfPageSize(t *testing.T) {
	t.Parallel()

	pageRows := []map[string]any{
		{"id": "repository:alpha", "name": "alpha", "path": "", "local_path": "", "remote_url": "", "repo_slug": "", "has_remote": false, "is_dependency": false},
	}
	// The graph has 42 repositories in total; the page limit is 1.
	graph := fakeGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if isRepositoryCountCypher(cypher) {
				return []map[string]any{{"total": int64(42)}}, nil
			}
			return pageRows, nil
		},
	}
	handler := &RepositoryHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=1&offset=0", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := body["count"], float64(1); got != want {
		t.Fatalf("count = %#v, want %#v (page slice)", got, want)
	}
	if got, want := body["total"], float64(42); got != want {
		t.Fatalf("total = %#v, want %#v (graph total independent of page)", got, want)
	}
}
