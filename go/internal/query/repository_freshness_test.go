// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// fakeRepositoryFreshnessReader is the query-package test double for
// RepositoryFreshnessReader, matching the fakeRepoGraphReader/
// fakePortContentStore convention already used by sibling repository route
// tests in this package.
type fakeRepositoryFreshnessReader struct {
	snapshot  status.RepositoryFreshnessSnapshot
	err       error
	gotRepoID string
}

func (f *fakeRepositoryFreshnessReader) ReadRepositoryFreshness(_ context.Context, repoID string) (status.RepositoryFreshnessSnapshot, error) {
	f.gotRepoID = repoID
	if f.err != nil {
		return status.RepositoryFreshnessSnapshot{}, f.err
	}
	return f.snapshot, nil
}

func repositoryFreshnessTestHandler(reader *fakeRepositoryFreshnessReader) *RepositoryHandler {
	return &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": repositoryStatsGraphRow(),
			},
		},
		Content:   fakePortContentStore{repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()}},
		Freshness: reader,
	}
}

func fullyBuiltRepositoryFreshnessSnapshot() status.RepositoryFreshnessSnapshot {
	activatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	return status.RepositoryFreshnessSnapshot{
		RepositoryID:  "repo-1",
		ScopeID:       "scope-1",
		Resolved:      true,
		ScopeKind:     "repository",
		HasGeneration: true,
		Generation: status.RepositoryFreshnessGeneration{
			ID: "gen-1", Status: "active", TriggerKind: "push", IsDelta: true, ActivatedAt: activatedAt,
		},
		ObservedCommit: "abc123",
		ObservedAt:     activatedAt.Add(-time.Minute),
		Stages:         status.RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
	}
}

// TestGetRepositoryFreshnessRendersCurrentVerdict verifies the happy path:
// every response field documented by issue #5143 is present with the
// expected shape when the resolved generation is fully built.
func TestGetRepositoryFreshnessRendersCurrentVerdict(t *testing.T) {
	t.Parallel()

	reader := &fakeRepositoryFreshnessReader{snapshot: fullyBuiltRepositoryFreshnessSnapshot()}
	handler := repositoryFreshnessTestHandler(reader)

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if reader.gotRepoID != "repo-1" {
		t.Fatalf("reader received repo id %q, want repo-1", reader.gotRepoID)
	}

	resp := decodeRepositoryStatsResponse(t, w)
	if got, want := resp["verdict"], "current"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
	if got, want := resp["scope_id"], "scope-1"; got != want {
		t.Fatalf("scope_id = %#v, want %#v", got, want)
	}
	if got, want := resp["observed_commit"], "abc123"; got != want {
		t.Fatalf("observed_commit = %#v, want %#v", got, want)
	}
	if resp["observed_at"] == nil || resp["observed_at"] == "" {
		t.Fatalf("observed_at = %#v, want a non-empty timestamp", resp["observed_at"])
	}
	if got, want := resp["scoped"], false; got != want {
		t.Fatalf("scoped = %#v, want %#v", got, want)
	}
	if resp["as_of"] == nil || resp["as_of"] == "" {
		t.Fatal("as_of missing from response")
	}

	repo := repositoryStatsRequireMap(t, resp, "repository")
	if got, want := repo["id"], "repo-1"; got != want {
		t.Fatalf("repository.id = %#v, want %#v", got, want)
	}

	generation := repositoryStatsRequireMap(t, resp, "generation")
	if got, want := generation["id"], "gen-1"; got != want {
		t.Fatalf("generation.id = %#v, want %#v", got, want)
	}
	if got, want := generation["status"], "active"; got != want {
		t.Fatalf("generation.status = %#v, want %#v", got, want)
	}
	if got, want := generation["trigger_kind"], "push"; got != want {
		t.Fatalf("generation.trigger_kind = %#v, want %#v", got, want)
	}
	if got, want := generation["is_delta"], true; got != want {
		t.Fatalf("generation.is_delta = %#v, want %#v", got, want)
	}
	if generation["activated_at"] == nil {
		t.Fatal("generation.activated_at missing")
	}

	stages := repositoryStatsRequireMap(t, resp, "stages")
	for _, key := range []string{"collected", "reduced", "projected", "materialized"} {
		if got, want := stages[key], true; got != want {
			t.Fatalf("stages.%s = %#v, want %#v", key, got, want)
		}
	}

	outstanding, ok := resp["outstanding_by_stage"].([]any)
	if !ok || len(outstanding) != 0 {
		t.Fatalf("outstanding_by_stage = %#v, want empty slice", resp["outstanding_by_stage"])
	}

	sharedEnrichment := repositoryStatsRequireMap(t, resp, "shared_enrichment")
	if got, want := sharedEnrichment["pending"], false; got != want {
		t.Fatalf("shared_enrichment.pending = %#v, want %#v", got, want)
	}
	if _, ok := sharedEnrichment["pending_domains"].([]any); !ok {
		t.Fatalf("shared_enrichment.pending_domains type = %T, want []any", sharedEnrichment["pending_domains"])
	}

	if got := resp["unobserved_push"]; got != nil {
		t.Fatalf("unobserved_push = %#v, want nil", got)
	}
}

// TestGetRepositoryFreshnessRendersEachVerdict smoke-tests every verdict
// branch renders end to end through the handler (the exhaustive branch
// coverage itself lives in status.ComputeRepositoryFreshnessVerdict's own
// tests).
func TestGetRepositoryFreshnessRendersEachVerdict(t *testing.T) {
	t.Parallel()

	building := fullyBuiltRepositoryFreshnessSnapshot()
	building.Stages.Reduced = false

	behind := fullyBuiltRepositoryFreshnessSnapshot()

	unobserved := fullyBuiltRepositoryFreshnessSnapshot()
	unobserved.UnobservedPush = &status.RepositoryFreshnessUnobservedPush{
		TargetSHA: "def456", Ref: "refs/heads/main", ReceivedAt: time.Date(2026, 7, 12, 3, 5, 0, 0, time.UTC),
	}

	unknown := status.RepositoryFreshnessSnapshot{RepositoryID: "repo-1", Resolved: false}

	tests := []struct {
		name           string
		snapshot       status.RepositoryFreshnessSnapshot
		expectedCommit string
		wantVerdict    string
	}{
		{name: "building", snapshot: building, wantVerdict: "building"},
		{name: "behind", snapshot: behind, expectedCommit: "def456", wantVerdict: "behind"},
		{name: "unobserved", snapshot: unobserved, wantVerdict: "unobserved"},
		{name: "unknown", snapshot: unknown, wantVerdict: "unknown"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := &fakeRepositoryFreshnessReader{snapshot: tt.snapshot}
			handler := repositoryFreshnessTestHandler(reader)
			mux := http.NewServeMux()
			handler.Mount(mux)

			path := "/api/v0/repositories/repo-1/freshness"
			if tt.expectedCommit != "" {
				path += "?expected_commit=" + tt.expectedCommit
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			resp := decodeRepositoryStatsResponse(t, w)
			if got, want := resp["verdict"], tt.wantVerdict; got != want {
				t.Fatalf("verdict = %#v, want %#v; body = %s", got, want, w.Body.String())
			}
			if tt.name == "unknown" {
				if resp["generation"] != nil {
					t.Fatalf("generation = %#v, want nil for an unresolved repository", resp["generation"])
				}
			}
		})
	}
}

// TestGetRepositoryFreshnessScopedAllowedRepositoryReturnsData verifies a
// scoped caller with the resolved repository in its grant set gets the same
// response a shared/admin caller would, with scoped=true.
func TestGetRepositoryFreshnessScopedAllowedRepositoryReturnsData(t *testing.T) {
	t.Parallel()

	reader := &fakeRepositoryFreshnessReader{snapshot: fullyBuiltRepositoryFreshnessSnapshot()}
	handler := repositoryFreshnessTestHandler(reader)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repo-1"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	resp := decodeRepositoryStatsResponse(t, w)
	if got, want := resp["scoped"], true; got != want {
		t.Fatalf("scoped = %#v, want %#v", got, want)
	}
	if got, want := resp["verdict"], "current"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
}

// TestGetRepositoryFreshnessScopedDeniedRepositoryReturns404 verifies a
// scoped caller with NO grant on the resolved repository id gets a 404 --
// consistent with sibling repository routes (resolveRepositoryPathSelector)
// -- never a 403 that would confirm the repository's existence, and the
// freshness reader is never invoked.
func TestGetRepositoryFreshnessScopedDeniedRepositoryReturns404(t *testing.T) {
	t.Parallel()

	reader := &fakeRepositoryFreshnessReader{snapshot: fullyBuiltRepositoryFreshnessSnapshot()}
	handler := repositoryFreshnessTestHandler(reader)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repo-other"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if reader.gotRepoID != "" {
		t.Fatal("freshness reader was invoked for a repository outside the caller's grant")
	}
}

// TestAuthMiddlewareWithScopedTokensAllowsRepositoryFreshnessRoute is the PR
// #5150 review regression (codex + carried-forward P1): the two scoped tests
// above mount the handler on a bare http.NewServeMux(), which bypasses
// AuthMiddlewareWithScopedTokens entirely and encoded a false green --
// GET /api/v0/repositories/{repo_id}/freshness was never added to
// scopedHTTPRouteSupportsTenantFilter (auth_scoped_routes.go), so a real
// scoped-token or browser-session caller got 403 from the middleware before
// getRepositoryFreshness's grant filtering (and the promised 404 parity)
// could ever run. This test routes through the real middleware, matching the
// #5137 pattern in auth_ingester_status_test.go
// (TestAuthMiddlewareWithScopedTokensAllowsIngesterStatusRoutes).
func TestAuthMiddlewareWithScopedTokensAllowsRepositoryFreshnessRoute(t *testing.T) {
	t.Parallel()

	newMiddlewareWrappedHandler := func(allowedRepositoryIDs []string) http.Handler {
		reader := &fakeRepositoryFreshnessReader{snapshot: fullyBuiltRepositoryFreshnessSnapshot()}
		handler := repositoryFreshnessTestHandler(reader)
		mux := http.NewServeMux()
		handler.Mount(mux)
		resolver := &fakeScopedTokenResolver{
			context: AuthContext{
				Mode:                 AuthModeScoped,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				SubjectClass:         "team",
				SubjectIDHash:        "sha256:team-a",
				PolicyRevisionHash:   "sha256:policy",
				AllowedRepositoryIDs: allowedRepositoryIDs,
			},
			ok: true,
		}
		return AuthMiddlewareWithScopedTokens("", resolver, mux)
	}

	t.Run("grant reaches the handler", func(t *testing.T) {
		t.Parallel()

		middleware := newMiddlewareWrappedHandler([]string{"repo-1"})
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
		req.Header.Set("Authorization", "Bearer scoped-token")
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
		}
		resp := decodeRepositoryStatsResponse(t, w)
		if got, want := resp["scoped"], true; got != want {
			t.Fatalf("scoped = %#v, want %#v", got, want)
		}
	})

	t.Run("no grant is a 404, not a 403", func(t *testing.T) {
		t.Parallel()

		middleware := newMiddlewareWrappedHandler([]string{"repo-other"})
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
		req.Header.Set("Authorization", "Bearer scoped-token")
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusNotFound; got != want {
			t.Fatalf("status = %d, want %d (grant filtering, not middleware 403); body = %s", got, want, w.Body.String())
		}
	})
}

// TestGetRepositoryFreshnessAcceptsArbitraryExpectedCommit verifies the
// optional expected_commit query parameter is treated as an opaque
// comparison string (no format validation) -- an arbitrary non-matching
// value still resolves to a 200 with verdict=behind rather than a 400.
func TestGetRepositoryFreshnessAcceptsArbitraryExpectedCommit(t *testing.T) {
	t.Parallel()

	reader := &fakeRepositoryFreshnessReader{snapshot: fullyBuiltRepositoryFreshnessSnapshot()}
	handler := repositoryFreshnessTestHandler(reader)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness?expected_commit=not-a-real-sha", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	resp := decodeRepositoryStatsResponse(t, w)
	if got, want := resp["verdict"], "behind"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
}

// TestGetRepositoryFreshnessUnknownRepositoryReturns404 verifies an unknown
// repository selector 404s before the freshness reader is ever consulted,
// matching sibling repository routes.
func TestGetRepositoryFreshnessUnknownRepositoryReturns404(t *testing.T) {
	t.Parallel()

	reader := &fakeRepositoryFreshnessReader{snapshot: fullyBuiltRepositoryFreshnessSnapshot()}
	handler := &RepositoryHandler{
		Neo4j:     fakeRepoGraphReader{},
		Content:   fakePortContentStore{},
		Freshness: reader,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/does-not-exist/freshness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if reader.gotRepoID != "" {
		t.Fatal("freshness reader was invoked for an unresolvable repository")
	}
}

// TestGetRepositoryFreshnessReaderNotConfiguredReturns503 verifies a nil
// Freshness reader is treated as not-configured, matching the sibling
// nil-reader checks on the status routes.
func TestGetRepositoryFreshnessReaderNotConfiguredReturns503(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

// TestGetRepositoryFreshnessReadErrorReturns500 verifies a storage read
// failure surfaces as a 500, not a silently empty snapshot.
func TestGetRepositoryFreshnessReadErrorReturns500(t *testing.T) {
	t.Parallel()

	reader := &fakeRepositoryFreshnessReader{err: errors.New("connection reset")}
	handler := repositoryFreshnessTestHandler(reader)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

// TestGetRepositoryFreshnessOutstandingByStageRendersRows verifies
// outstanding_by_stage renders each (stage, status, count) row from the
// snapshot.
func TestGetRepositoryFreshnessOutstandingByStageRendersRows(t *testing.T) {
	t.Parallel()

	snapshot := fullyBuiltRepositoryFreshnessSnapshot()
	snapshot.Stages.Reduced = false
	snapshot.Outstanding = []status.RepositoryFreshnessOutstanding{
		{Stage: "reducer", Status: "pending", Count: 2},
		{Stage: "reducer", Status: "retrying", Count: 1},
	}

	reader := &fakeRepositoryFreshnessReader{snapshot: snapshot}
	handler := repositoryFreshnessTestHandler(reader)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	resp := decodeRepositoryStatsResponse(t, w)
	outstanding, ok := resp["outstanding_by_stage"].([]any)
	if !ok || len(outstanding) != 2 {
		t.Fatalf("outstanding_by_stage = %#v, want 2 rows", resp["outstanding_by_stage"])
	}
	first, ok := outstanding[0].(map[string]any)
	if !ok {
		t.Fatalf("outstanding_by_stage[0] type = %T, want map[string]any", outstanding[0])
	}
	if got, want := first["stage"], "reducer"; got != want {
		t.Fatalf("outstanding_by_stage[0].stage = %#v, want %#v", got, want)
	}
	if got, want := first["count"], float64(2); got != want {
		t.Fatalf("outstanding_by_stage[0].count = %#v, want %#v", got, want)
	}
}
