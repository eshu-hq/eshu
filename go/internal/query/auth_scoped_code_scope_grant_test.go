// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// #5167 code family: a scoped token may name a repository two ways, and only
// one of them is the id these reads compare against.
//
// A grant can carry the canonical repository id ("repo://tenant-a/svc") or the
// id of the ingestion scope that owns it
// ("git-repository-scope:repo://tenant-a/svc"). Content rows carry the
// canonical id in repo_id and graph Repository nodes carry it in `id`, so a
// scope-only grant pushed into `repo_id = ANY($n)` or into
// `r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` matches
// nothing and the caller gets an empty page for a repository they may read.
// The identity distinction, and the rebinding that closes it, are the #5052
// keyword-search defect (docs/internal/evidence/5052-keyword-search-scope-id.md).
//
// These tests run every family of the batch through a scope-only grant. They
// are red on a build where the grant is not resolved: the content routes bind
// a list holding only the scope id, and the graph routes' evaluated predicate
// admits no row.

// codeGrantScopeOnlyAuthContext grants exactly one git repository ingestion
// scope and no canonical repository id at all -- the shape a token gets when
// the operator granted the scope the collector created.
func codeGrantScopeOnlyAuthContext(repoIDs []string) AuthContext {
	scopeIDs := make([]string, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		scopeIDs = append(scopeIDs, "git-repository-scope:"+repoID)
	}
	return AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: scopeIDs,
	}
}

func TestCodeContentRoutesResolveAScopeOnlyGrantToItsRepository(t *testing.T) {
	t.Parallel()

	for _, route := range codeContentGrantRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := route.newStore()
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := codeGrantScopeOnlyAuthContext([]string{codeGrantGrantedRepo})
			req := newCodeGrantRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if got := store.boundRepositoryIDs(); !slices.Contains(got, codeGrantGrantedRepo) {
				t.Fatalf("AllowedRepositoryIDs = %#v, want the canonical id %q the granted scope names", got, codeGrantGrantedRepo)
			}
			body := rec.Body.String()
			if !strings.Contains(body, codeGrantGrantedRepo) {
				t.Fatalf("response missing the granted repository %q: %s", codeGrantGrantedRepo, body)
			}
			if strings.Contains(body, codeGrantOtherRepo) {
				t.Fatalf("response leaked the out-of-grant repository %q: %s", codeGrantOtherRepo, body)
			}
		})
	}
}

func TestDeadCodeRoutesResolveAScopeOnlyGrantToItsRepository(t *testing.T) {
	t.Parallel()

	for _, route := range deadCodeGrantRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := &deadCodeGrantContentStore{}
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := codeGrantScopeOnlyAuthContext([]string{codeGrantGrantedRepo})
			req := newCodeGrantRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if got := store.bound; !slices.Contains(got, codeGrantGrantedRepo) {
				t.Fatalf("AllowedRepositoryIDs = %#v, want the canonical id %q the granted scope names", got, codeGrantGrantedRepo)
			}
			body := rec.Body.String()
			if !strings.Contains(body, codeGrantGrantedRepo) {
				t.Fatalf("response missing the granted repository %q: %s", codeGrantGrantedRepo, body)
			}
			if strings.Contains(body, codeGrantOtherRepo) {
				t.Fatalf("response leaked the out-of-grant repository %q: %s", codeGrantOtherRepo, body)
			}
		})
	}
}

func TestComplexityListResolvesAScopeOnlyGrantToItsRepository(t *testing.T) {
	t.Parallel()

	graph := &evaluatingRepositoryGraph{
		seeds:             complexityListSeeds(),
		repositoryColumns: repositoryProjectedColumns(),
	}
	auth := codeGrantScopeOnlyAuthContext([]string{codeGrantGrantedRepo})
	rec := runGraphGrantRoute(t, graph, "/api/v0/code/complexity", map[string]any{}, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedFunction) {
		t.Fatalf("scope-only grant returned no row for the repository it names: %s", body)
	}
	for _, leaked := range []string{codeGrantUngrantedFunction, codeGrantOrphanFunction, codeGrantOtherRepo} {
		if strings.Contains(body, leaked) {
			t.Fatalf("scope-only complexity list leaked %q: %s", leaked, body)
		}
	}
}

func TestCodeQualityInspectResolvesAScopeOnlyGrantToItsRepository(t *testing.T) {
	t.Parallel()

	graph := &evaluatingRepositoryGraph{
		seeds:             codeQualityInspectSeeds(),
		repositoryColumns: repositoryProjectedColumns(),
	}
	auth := codeGrantScopeOnlyAuthContext([]string{codeGrantGrantedRepo})
	rec := runGraphGrantRoute(
		t,
		graph,
		"/api/v0/code/quality/inspect",
		map[string]any{"check": "refactoring_candidates"},
		&auth,
	)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedFunction) {
		t.Fatalf("scope-only grant returned no row for the repository it names: %s", body)
	}
	if strings.Contains(body, codeGrantUngrantedFunction) {
		t.Fatalf("scope-only quality inspect leaked %q: %s", codeGrantUngrantedFunction, body)
	}
}

// TestCallGraphMetricsAcceptsAScopeOnlyGrantsRepository covers the routes where
// repo_id is mandatory: the selector resolves it through the same grant, so an
// unresolved scope grant refuses the request outright rather than answering an
// empty page.
func TestCallGraphMetricsAcceptsAScopeOnlyGrantsRepository(t *testing.T) {
	t.Parallel()

	auth := codeGrantScopeOnlyAuthContext([]string{codeGrantGrantedRepo})
	_, _, status := captureCallGraphMetricsCypher(t, &auth, callGraphMetricsGrantBody())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d for a caller granted the scope that owns %q", status, http.StatusOK, codeGrantGrantedRepo)
	}
}
