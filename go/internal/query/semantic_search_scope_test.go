// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type fakeSemanticSearchScopeResolver struct {
	scopeID string
	err     error
	calls   int
	repoID  string
}

type recordingSemanticSearchScopeQueryer struct {
	rows  *scriptedRows
	query string
	args  []any
}

func (q *recordingSemanticSearchScopeQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (pgstatus.Rows, error) {
	q.query = query
	q.args = args
	return q.rows, nil
}

func (r *fakeSemanticSearchScopeResolver) ResolveSemanticSearchScope(
	_ context.Context,
	repoID string,
) (string, error) {
	r.calls++
	r.repoID = repoID
	return r.scopeID, r.err
}

func TestSemanticSearchHandlerResolvesAuthorizedRepositoryToDistinctScope(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	resolver := &fakeSemanticSearchScopeResolver{scopeID: "git-repository-scope:repo-payments"}
	handler := &SemanticSearchHandler{
		Index:         index,
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repository:r_payments",
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:r_payments"},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := resolver.calls, 1; got != want {
		t.Fatalf("resolver calls = %d, want %d", got, want)
	}
	if got, want := resolver.repoID, "repository:r_payments"; got != want {
		t.Fatalf("resolver repo id = %q, want %q", got, want)
	}
	if got, want := index.query.ScopeID, "git-repository-scope:repo-payments"; got != want {
		t.Fatalf("index scope id = %q, want %q", got, want)
	}
	if got, want := index.query.RepoID, "repository:r_payments"; got != want {
		t.Fatalf("index repo id = %q, want %q", got, want)
	}
}

func TestSemanticSearchHandlerRejectsOutOfGrantBeforeScopeResolution(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	resolver := &fakeSemanticSearchScopeResolver{scopeID: "git-repository-scope:repo-payments"}
	handler := &SemanticSearchHandler{
		Index:         index,
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repository:r_payments",
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:r_other"},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0 before authorization", resolver.calls)
	}
	if index.calls != 0 {
		t.Fatalf("index calls = %d, want 0 before authorization", index.calls)
	}
}

func TestSemanticSearchHandlerRejectsAmbiguousRepositoryScope(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	resolver := &fakeSemanticSearchScopeResolver{err: ErrSemanticSearchScopeAmbiguous}
	handler := &SemanticSearchHandler{
		Index:         index,
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repository:r_payments",
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:r_payments"},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeAmbiguous {
		t.Fatalf("error = %#v, want code %q", envelope.Error, ErrorCodeAmbiguous)
	}
	if index.calls != 0 {
		t.Fatalf("index calls = %d, want 0 for ambiguous scope", index.calls)
	}
}

func TestPostgresSemanticSearchScopeResolverUsesExactCanonicalRepositoryID(t *testing.T) {
	t.Parallel()

	db := &recordingSemanticSearchScopeQueryer{rows: &scriptedRows{
		data: [][]any{{"git-repository-scope:repo-payments"}},
	}}
	resolver := PostgresSemanticSearchScopeResolver{db: db}

	scopeID, err := resolver.ResolveSemanticSearchScope(context.Background(), "repository:r_payments")
	if err != nil {
		t.Fatalf("ResolveSemanticSearchScope() error = %v", err)
	}
	if got, want := scopeID, "git-repository-scope:repo-payments"; got != want {
		t.Fatalf("scope id = %q, want %q", got, want)
	}
	for _, fragment := range []string{
		"scope_kind = 'repository'",
		"active_generation_id IS NOT NULL",
		"payload->>'repo_id' = $1",
		"LIMIT 2",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Errorf("resolver query missing %q:\n%s", fragment, db.query)
		}
	}
	if strings.Contains(db.query, "LIKE") {
		t.Errorf("resolver query must use exact repository identity, not prefix matching:\n%s", db.query)
	}
	if got, want := db.args, []any{"repository:r_payments"}; !semanticSearchAnySlicesEqual(got, want) {
		t.Fatalf("query args = %#v, want %#v", got, want)
	}
}

func TestPostgresSemanticSearchScopeResolverRejectsMultipleActiveScopes(t *testing.T) {
	t.Parallel()

	resolver := PostgresSemanticSearchScopeResolver{db: &recordingSemanticSearchScopeQueryer{
		rows: &scriptedRows{data: [][]any{
			{"git-repository-scope:repo-payments-a"},
			{"git-repository-scope:repo-payments-b"},
		}},
	}}

	_, err := resolver.ResolveSemanticSearchScope(context.Background(), "repository:r_payments")
	if !errors.Is(err, ErrSemanticSearchScopeAmbiguous) {
		t.Fatalf("ResolveSemanticSearchScope() error = %v, want %v", err, ErrSemanticSearchScopeAmbiguous)
	}
}

func semanticSearchAnySlicesEqual(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
