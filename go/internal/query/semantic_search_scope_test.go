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
	scopeID          string
	directRepoID     string
	err              error
	calls            int
	directScopeCalls int
	repoID           string
	directScopeID    string
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

func (r *fakeSemanticSearchScopeResolver) ResolveSemanticSearchRepositoryForScope(
	_ context.Context,
	scopeID string,
) (string, error) {
	r.directScopeCalls++
	r.directScopeID = scopeID
	return r.directRepoID, r.err
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

func TestSemanticSearchHandlerUsesDirectGrantedScopeAndCanonicalRepository(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	resolver := &fakeSemanticSearchScopeResolver{directRepoID: "repository:r_payments"}
	handler := &SemanticSearchHandler{
		Index:         index,
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "git-repository-scope:repo-payments",
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		AllowedScopeIDs: []string{"git-repository-scope:repo-payments"},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := resolver.directScopeCalls, 1; got != want {
		t.Fatalf("direct scope resolver calls = %d, want %d", got, want)
	}
	if got, want := resolver.directScopeID, "git-repository-scope:repo-payments"; got != want {
		t.Fatalf("direct scope id = %q, want %q", got, want)
	}
	if got, want := resolver.calls, 0; got != want {
		t.Fatalf("canonical repository resolver calls = %d, want %d", got, want)
	}
	if got, want := index.query.ScopeID, "git-repository-scope:repo-payments"; got != want {
		t.Fatalf("index scope id = %q, want %q", got, want)
	}
	if got, want := index.query.RepoID, "repository:r_payments"; got != want {
		t.Fatalf("index repo id = %q, want %q", got, want)
	}
}

func TestSemanticSearchHandlerAllScopesUsesDirectActiveScope(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	resolver := &fakeSemanticSearchScopeResolver{directRepoID: "repository:r_payments"}
	handler := &SemanticSearchHandler{
		Index:         index,
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "git-repository-scope:repo-payments",
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := resolver.directScopeCalls, 1; got != want {
		t.Fatalf("direct scope resolver calls = %d, want %d", got, want)
	}
	if got, want := resolver.directScopeID, "git-repository-scope:repo-payments"; got != want {
		t.Fatalf("direct scope id = %q, want %q", got, want)
	}
	if got, want := resolver.calls, 0; got != want {
		t.Fatalf("canonical repository resolver calls = %d, want %d", got, want)
	}
	if got, want := index.calls, 1; got != want {
		t.Fatalf("index calls = %d, want %d", got, want)
	}
	if got, want := index.query.ScopeID, "git-repository-scope:repo-payments"; got != want {
		t.Fatalf("index scope id = %q, want %q", got, want)
	}
	if got, want := index.query.RepoID, "repository:r_payments"; got != want {
		t.Fatalf("index repo id = %q, want %q", got, want)
	}
}

func TestSemanticSearchHandlerAllScopesCanonicalRepositorySkipsDirectScopeLookup(t *testing.T) {
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
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := resolver.directScopeCalls, 0; got != want {
		t.Fatalf("direct scope resolver calls = %d, want %d", got, want)
	}
	if got, want := resolver.calls, 1; got != want {
		t.Fatalf("canonical repository resolver calls = %d, want %d", got, want)
	}
	if got, want := resolver.repoID, "repository:r_payments"; got != want {
		t.Fatalf("canonical repository id = %q, want %q", got, want)
	}
	if got, want := index.query.ScopeID, "git-repository-scope:repo-payments"; got != want {
		t.Fatalf("index scope id = %q, want %q", got, want)
	}
	if got, want := index.query.RepoID, "repository:r_payments"; got != want {
		t.Fatalf("index repo id = %q, want %q", got, want)
	}
}

func TestSemanticSearchHandlerDoesNotReadIndexForStaleDirectScope(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	resolver := &fakeSemanticSearchScopeResolver{}
	handler := &SemanticSearchHandler{
		Index:         index,
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "git-repository-scope:repo-stale",
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		AllowedScopeIDs: []string{"git-repository-scope:repo-stale"},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := resolver.directScopeCalls, 1; got != want {
		t.Fatalf("direct scope resolver calls = %d, want %d", got, want)
	}
	if got := index.calls; got != 0 {
		t.Fatalf("index calls = %d, want 0 for stale direct scope", got)
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

func TestPostgresSemanticSearchScopeResolverValidatesDirectActiveScope(t *testing.T) {
	t.Parallel()

	db := &recordingSemanticSearchScopeQueryer{rows: &scriptedRows{
		data: [][]any{{"repository:r_payments"}},
	}}
	resolver := PostgresSemanticSearchScopeResolver{db: db}

	repoID, err := resolver.ResolveSemanticSearchRepositoryForScope(
		context.Background(),
		"git-repository-scope:repo-payments",
	)
	if err != nil {
		t.Fatalf("ResolveSemanticSearchRepositoryForScope() error = %v", err)
	}
	if got, want := repoID, "repository:r_payments"; got != want {
		t.Fatalf("repo id = %q, want %q", got, want)
	}
	for _, fragment := range []string{
		"scope_kind = 'repository'",
		"active_generation_id IS NOT NULL",
		"scope_id = $1",
		"LIMIT 1",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Errorf("direct scope query missing %q:\n%s", fragment, db.query)
		}
	}
	if got, want := db.args, []any{"git-repository-scope:repo-payments"}; !semanticSearchAnySlicesEqual(got, want) {
		t.Fatalf("query args = %#v, want %#v", got, want)
	}
}

func TestPostgresSemanticSearchScopeResolverRejectsBlankDirectScopeMetadata(t *testing.T) {
	t.Parallel()

	resolver := PostgresSemanticSearchScopeResolver{db: &recordingSemanticSearchScopeQueryer{
		rows: &scriptedRows{data: [][]any{{""}}},
	}}

	_, err := resolver.ResolveSemanticSearchRepositoryForScope(
		context.Background(),
		"git-repository-scope:repo-malformed",
	)
	if err == nil || !strings.Contains(err.Error(), "has no canonical repository id") {
		t.Fatalf("ResolveSemanticSearchRepositoryForScope() error = %v, want malformed metadata error", err)
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
