// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordingCodeownersGraphReader is a GraphQuery test double covering both
// the paginated ownership list (Run) and the effective-owner last-match
// lookup (RunSingle), recording the cypher/params each saw.
type recordingCodeownersGraphReader struct {
	runRows        []map[string]any
	runErr         error
	lastRunCypher  string
	lastRunParams  map[string]any
	sawDeadline    bool
	singleRow      map[string]any
	singleErr      error
	lastSingleArgs map[string]any
}

func (r *recordingCodeownersGraphReader) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	r.lastRunCypher = cypher
	r.lastRunParams = params
	_, r.sawDeadline = ctx.Deadline()
	if r.runErr != nil {
		return nil, r.runErr
	}
	return r.runRows, nil
}

func (r *recordingCodeownersGraphReader) RunSingle(
	_ context.Context,
	_ string,
	params map[string]any,
) (map[string]any, error) {
	r.lastSingleArgs = params
	if r.singleErr != nil {
		return nil, r.singleErr
	}
	return r.singleRow, nil
}

func newCodeownersOwnershipMux(neo4j GraphQuery, correlations ServiceCatalogCorrelationStore) *http.ServeMux {
	handler := &CodeownersOwnershipHandler{Neo4j: neo4j, Correlations: correlations}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func TestCodeownersOwnershipRequiresRepositoryID(t *testing.T) {
	t.Parallel()

	mux := newCodeownersOwnershipMux(&recordingCodeownersGraphReader{}, &fakeCodeownersCorrelationStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestCodeownersOwnershipRejectsInvalidLimitAndHalfCursor(t *testing.T) {
	t.Parallel()

	mux := newCodeownersOwnershipMux(&recordingCodeownersGraphReader{}, &fakeCodeownersCorrelationStore{})

	cases := map[string]string{
		"zero limit":            "/api/v0/codeowners/ownership?repository_id=repo-1&limit=0",
		"over max limit":        "/api/v0/codeowners/ownership?repository_id=repo-1&limit=201",
		"half cursor (pattern)": "/api/v0/codeowners/ownership?repository_id=repo-1&after_pattern=*.go",
		"half cursor (ref)":     "/api/v0/codeowners/ownership?repository_id=repo-1&after_ref=@org/team",
	}
	for name, target := range cases {
		name, target := name, target
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestCodeownersOwnershipBackendUnavailableWhenGraphMissing(t *testing.T) {
	t.Parallel()

	handler := &CodeownersOwnershipHandler{Correlations: &fakeCodeownersCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id=repo-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestCodeownersOwnershipDefaultsReturnBoundedPageAndEffectiveOwner(t *testing.T) {
	t.Parallel()

	graph := &recordingCodeownersGraphReader{
		runRows: []map[string]any{
			{"pattern": "*.go", "source_path": "CODEOWNERS", "order_index": int64(0), "owner_ref": "@org/team-a"},
		},
		singleRow: map[string]any{"owner_ref": "@org/team-a"},
	}
	correlations := &fakeCodeownersCorrelationStore{}
	mux := newCodeownersOwnershipMux(graph, correlations)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id=repo-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !graph.sawDeadline {
		t.Fatal("codeowners ownership query context has no deadline; graph reads need a server-side read budget")
	}
	if got, want := graph.lastRunParams["repo_id"], "repo-1"; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := graph.lastRunParams["limit"], 51; got != want {
		t.Fatalf("params[limit] = %#v, want %#v (limit+1 truncation probe)", got, want)
	}

	var resp struct {
		Ownership      []CodeownersOwnershipRow `json:"ownership"`
		RepositoryID   string                   `json:"repository_id"`
		Count          int                      `json:"count"`
		Limit          int                      `json:"limit"`
		Truncated      bool                     `json:"truncated"`
		EffectiveOwner EffectiveRepositoryOwner `json:"effective_owner"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.RepositoryID, "repo-1"; got != want {
		t.Fatalf("repository_id = %q, want %q", got, want)
	}
	if got, want := len(resp.Ownership), 1; got != want {
		t.Fatalf("len(ownership) = %d, want %d", got, want)
	}
	row := resp.Ownership[0]
	if got, want := row.Pattern, "*.go"; got != want {
		t.Fatalf("pattern = %q, want %q", got, want)
	}
	if got, want := row.OwnerRef, "@org/team-a"; got != want {
		t.Fatalf("owner_ref = %q, want %q", got, want)
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false")
	}
	if want := (EffectiveRepositoryOwner{OwnerRef: "@org/team-a", Source: EffectiveOwnerSourceCodeowners}); resp.EffectiveOwner != want {
		t.Fatalf("effective_owner = %+v, want %+v", resp.EffectiveOwner, want)
	}
}

func TestCodeownersOwnershipEffectiveOwnerPrefersManifestSource(t *testing.T) {
	t.Parallel()

	graph := &recordingCodeownersGraphReader{singleRow: map[string]any{"owner_ref": "@org/team-codeowners"}}
	correlations := &fakeCodeownersCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			{RepositoryID: "repo-1", OwnerRef: "@org/team-manifest", Outcome: "exact"},
		},
	}
	mux := newCodeownersOwnershipMux(graph, correlations)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id=repo-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		EffectiveOwner EffectiveRepositoryOwner `json:"effective_owner"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if want := (EffectiveRepositoryOwner{OwnerRef: "@org/team-manifest", Source: EffectiveOwnerSourceServiceCatalog}); resp.EffectiveOwner != want {
		t.Fatalf("effective_owner = %+v, want %+v", resp.EffectiveOwner, want)
	}
}

func TestCodeownersOwnershipEffectiveOwnerEmptyWhenUnresolved(t *testing.T) {
	t.Parallel()

	graph := &recordingCodeownersGraphReader{}
	correlations := &fakeCodeownersCorrelationStore{}
	mux := newCodeownersOwnershipMux(graph, correlations)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id=repo-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		EffectiveOwner EffectiveRepositoryOwner `json:"effective_owner"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if want := (EffectiveRepositoryOwner{}); resp.EffectiveOwner != want {
		t.Fatalf("effective_owner = %+v, want zero value %+v", resp.EffectiveOwner, want)
	}
}

func TestCodeownersOwnershipTruncatesAndEmitsKeysetCursor(t *testing.T) {
	t.Parallel()

	graph := &recordingCodeownersGraphReader{
		runRows: []map[string]any{
			{"pattern": "*.go", "source_path": "CODEOWNERS", "order_index": int64(0), "owner_ref": "@org/team-a"},
			{"pattern": "*.md", "source_path": "CODEOWNERS", "order_index": int64(1), "owner_ref": "@org/team-b"},
		},
	}
	mux := newCodeownersOwnershipMux(graph, &fakeCodeownersCorrelationStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id=repo-1&limit=1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := graph.lastRunParams["limit"], 2; got != want {
		t.Fatalf("params[limit] = %#v, want %#v (limit+1 truncation probe)", got, want)
	}

	var resp struct {
		Ownership  []CodeownersOwnershipRow `json:"ownership"`
		Truncated  bool                     `json:"truncated"`
		NextCursor struct {
			AfterOrderIndex int    `json:"after_order_index"`
			AfterPattern    string `json:"after_pattern"`
			AfterRef        string `json:"after_ref"`
		} `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Ownership), 1; got != want {
		t.Fatalf("len(ownership) = %d, want %d", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor.AfterOrderIndex, 0; got != want {
		t.Fatalf("next_cursor.after_order_index = %d, want %d", got, want)
	}
	if got, want := resp.NextCursor.AfterPattern, "*.go"; got != want {
		t.Fatalf("next_cursor.after_pattern = %q, want %q", got, want)
	}
	if got, want := resp.NextCursor.AfterRef, "@org/team-a"; got != want {
		t.Fatalf("next_cursor.after_ref = %q, want %q", got, want)
	}
}

func TestCodeownersOwnershipCursorThreadsKeysetParams(t *testing.T) {
	t.Parallel()

	graph := &recordingCodeownersGraphReader{}
	mux := newCodeownersOwnershipMux(graph, &fakeCodeownersCorrelationStore{})

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/codeowners/ownership?repository_id=repo-1&after_order_index=2&after_pattern=*.go&after_ref=@org/team-a&limit=5",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := graph.lastRunParams["after_order_index"], 2; got != want {
		t.Fatalf("params[after_order_index] = %#v, want %#v", got, want)
	}
	if got, want := graph.lastRunParams["after_pattern"], "*.go"; got != want {
		t.Fatalf("params[after_pattern] = %#v, want %#v", got, want)
	}
	if got, want := graph.lastRunParams["after_ref"], "@org/team-a"; got != want {
		t.Fatalf("params[after_ref] = %#v, want %#v", got, want)
	}
}
