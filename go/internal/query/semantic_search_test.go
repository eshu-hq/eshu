package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestSemanticSearchHandlerReturnsBoundedTruthLabeledResults(t *testing.T) {
	t.Parallel()

	store := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{
			{
				FactID:       "fact:searchdoc:payments",
				ScopeID:      "repo-payments",
				GenerationID: "gen-active",
				Document: semanticSearchDocumentFixture(
					"searchdoc:payments",
					"repo-payments",
					"Payments runbook",
					"payment runbook ownership escalation",
				),
			},
			{
				FactID:       "fact:searchdoc:billing",
				ScopeID:      "repo-payments",
				GenerationID: "gen-active",
				Document: semanticSearchDocumentFixture(
					"searchdoc:billing",
					"repo-payments",
					"Billing checklist",
					"billing invoice reconciliation",
				),
			},
		},
	}
	handler := &SemanticSearchHandler{Documents: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      1,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.calls, 1; got != want {
		t.Fatalf("store calls = %d, want %d", got, want)
	}
	if got, want := store.filter.ScopeID, "repo-payments"; got != want {
		t.Fatalf("filter.ScopeID = %q, want %q", got, want)
	}
	if got, want := store.filter.RepoID, "repo-payments"; got != want {
		t.Fatalf("filter.RepoID = %q, want %q", got, want)
	}
	if got, want := store.filter.Limit, 500; got != want {
		t.Fatalf("filter.Limit = %d, want %d", got, want)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope = nil, want search truth")
	}
	if got, want := envelope.Truth.Capability, semanticSearchCapability; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisHybrid; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["query"], "payment runbook"; got != want {
		t.Fatalf("query = %#v, want %#v", got, want)
	}
	if got, want := data["search_mode"], "keyword"; got != want {
		t.Fatalf("search_mode = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := data["indexed_document_count"], float64(2); got != want {
		t.Fatalf("indexed_document_count = %#v, want %#v", got, want)
	}
	results := data["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result := results[0].(map[string]any)
	if got, want := result["search_method"], "bm25"; got != want {
		t.Fatalf("result.search_method = %#v, want %#v", got, want)
	}
	document := result["document"].(map[string]any)
	if got, want := document["id"], "searchdoc:payments"; got != want {
		t.Fatalf("document.id = %#v, want %#v", got, want)
	}
	if _, ok := document["ID"]; ok {
		t.Fatalf("document leaked Go field casing: %#v", document)
	}
	truthScope := result["truth_scope"].(map[string]any)
	if got, want := truthScope["level"], "derived"; got != want {
		t.Fatalf("truth_scope.level = %#v, want %#v", got, want)
	}
	freshness := result["freshness"].(map[string]any)
	if got, want := freshness["state"], "fresh"; got != want {
		t.Fatalf("freshness.state = %#v, want %#v", got, want)
	}
}

func TestSemanticSearchHandlerScopedEmptyGrantReturnsEmptyWithoutRead(t *testing.T) {
	t.Parallel()

	store := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{{
			Document: semanticSearchDocumentFixture(
				"searchdoc:out-of-scope",
				"repo-payments",
				"Payments",
				"payment runbook",
			),
		}},
	}
	handler := &SemanticSearchHandler{Documents: store, Profile: ProfileProduction}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0 for empty scoped grant", store.calls)
	}
	data := semanticSearchEnvelopeData(t, rec)
	results := data["results"].([]any)
	if got := len(results); got != 0 {
		t.Fatalf("len(results) = %d, want 0", got)
	}
	if got, want := data["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestSemanticSearchHandlerScopedGrantRejectsOutOfGrantRepositoryBeforeRead(t *testing.T) {
	t.Parallel()

	store := &fakeSemanticSearchDocumentStore{}
	handler := &SemanticSearchHandler{Documents: store, Profile: ProfileProduction}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-infra"},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0 for out-of-grant repository", store.calls)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeNotFound {
		t.Fatalf("error = %#v, want not_found", envelope.Error)
	}
}

func TestSemanticSearchHandlerFiltersStoreRowsToRequestedRepository(t *testing.T) {
	t.Parallel()

	outOfRepo := semanticSearchDocumentFixture(
		"searchdoc:secret",
		"repo-secret",
		"Secret runbook",
		"secret service incident playbook",
	)
	outOfRepo.AccessScope = searchdocs.AccessScope{RepoID: "repo-secret"}
	outOfRepo.GraphHandles = []searchdocs.GraphHandle{
		{Kind: "repository", ID: "repo-secret"},
		{Kind: "service", ID: "svc-payments"},
	}
	store := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{
			{Document: semanticSearchDocumentFixture("searchdoc:payments", "repo-payments", "Payments", "payment runbook")},
			{Document: outOfRepo},
		},
	}
	handler := &SemanticSearchHandler{Documents: store, Profile: ProfileProduction}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"service_id": "svc-payments",
		"query":      "secret",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	results := data["results"].([]any)
	if got := len(results); got != 0 {
		t.Fatalf("len(results) = %d, want 0; results = %#v", got, results)
	}
}

func TestSemanticSearchHandlerRejectsUnboundedRequestsBeforeRead(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "missing limit",
			body: map[string]any{
				"repo_id":    "repo-payments",
				"query":      "payment runbook",
				"mode":       "keyword",
				"timeout_ms": 250,
			},
			want: "limit is required",
		},
		{
			name: "missing timeout",
			body: map[string]any{
				"repo_id": "repo-payments",
				"query":   "payment runbook",
				"mode":    "keyword",
				"limit":   5,
			},
			want: "timeout is required",
		},
		{
			name: "missing mode",
			body: map[string]any{
				"repo_id":    "repo-payments",
				"query":      "payment runbook",
				"limit":      5,
				"timeout_ms": 250,
			},
			want: "mode is invalid",
		},
		{
			name: "missing repository",
			body: map[string]any{
				"query":      "payment runbook",
				"mode":       "keyword",
				"limit":      5,
				"timeout_ms": 250,
			},
			want: "repo_id is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeSemanticSearchDocumentStore{}
			handler := &SemanticSearchHandler{Documents: store, Profile: ProfileProduction}
			rec := httptest.NewRecorder()

			handler.search(rec, semanticSearchHTTPRequest(t, tc.body))

			if got, want := rec.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if store.calls != 0 {
				t.Fatalf("store calls = %d, want 0 for invalid request", store.calls)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body = %s, want substring %q", rec.Body.String(), tc.want)
			}
		})
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsSemanticSearchRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-payments"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v0/search/semantic", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

type fakeSemanticSearchDocumentStore struct {
	rows   []semanticSearchDocumentRow
	filter semanticSearchDocumentFilter
	calls  int
}

func (s *fakeSemanticSearchDocumentStore) ListActiveDocuments(
	_ context.Context,
	filter semanticSearchDocumentFilter,
) ([]semanticSearchDocumentRow, error) {
	s.calls++
	s.filter = filter
	return append([]semanticSearchDocumentRow(nil), s.rows...), nil
}

func semanticSearchHTTPRequest(t *testing.T, body map[string]any) *http.Request {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/search/semantic", strings.NewReader(string(encoded)))
	req.Header.Set("Accept", EnvelopeMIMEType)
	return req
}

func semanticSearchEnvelopeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data = %T, want map[string]any", envelope.Data)
	}
	return data
}

func semanticSearchDocumentFixture(id string, repoID string, title string, contextText string) searchdocs.Document {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	return searchdocs.Document{
		ID:          id,
		RepoID:      repoID,
		SourceKind:  searchdocs.SourceKindRuntimeSummary,
		Title:       title,
		Path:        "docs/runbook.md",
		ContextText: contextText,
		UpdatedAt:   now,
		TruthScope: searchdocs.TruthScope{
			Level: searchdocs.TruthLevelDerived,
			Basis: searchdocs.TruthBasisReadModel,
		},
		Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		AccessScope: searchdocs.AccessScope{RepoID: repoID},
		GraphHandles: []searchdocs.GraphHandle{
			{Kind: "repository", ID: repoID},
			{Kind: "service", ID: "svc-payments"},
		},
		Labels: []string{"runtime", "payments"},
		Provenance: searchdocs.Provenance{
			SourceTable: "service_runtime_summaries",
			SourceIDs:   []string{id},
		},
	}
}
