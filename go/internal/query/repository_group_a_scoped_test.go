// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// groupARepositoryRoutes is the #5167 Group A inventory: five already-filtered
// single-repository GET routes that resolve through
// resolveRepositorySelectorExactForAccess (via resolveRepositoryPathSelector /
// resolveRepositoryStatsPathSelector) exactly like
// GET /api/v0/repositories/{repo_id}/freshness (#5143, #5150). Each needed only
// the allowlist matcher, the OpenAPI marker, and the completeness-ledger entry
// -- the handler's own grant filtering already worked.
var groupARepositoryRoutes = []struct {
	name string
	path string
}{
	{name: "stats", path: "/api/v0/repositories/repo-1/stats"},
	{name: "context", path: "/api/v0/repositories/repo-1/context"},
	{name: "story", path: "/api/v0/repositories/repo-1/story"},
	{name: "coverage", path: "/api/v0/repositories/repo-1/coverage"},
	{name: "tree", path: "/api/v0/repositories/repo-1/tree"},
}

// groupARepositoryTestHandler builds one RepositoryHandler whose fakes satisfy
// all five Group A routes at once: a base repository-lookup row for
// stats/context/story/tree (repositoryStatsRepositoryRef and
// requireContextOverview both resolve off the same
// "MATCH (r:Repository {id: $repo_id})" fragment), content-store coverage for
// the coverage route, and a repository file for the tree listing.
func groupARepositoryTestHandler() *RepositoryHandler {
	return &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": repositoryStatsGraphRow(),
			},
		},
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			coverage: RepositoryContentCoverage{
				Available:   true,
				FileCount:   1,
				EntityCount: 0,
			},
			repoFiles: []FileContent{
				{RepoID: "repo-1", RelativePath: "README.md", CommitSHA: "abc123", LineCount: 1, Language: "markdown"},
			},
		},
	}
}

// TestGroupARepositoryRoutesScopedAllowedRepositoryReturnsData verifies a
// scoped caller with the resolved repository in its grant set gets a normal
// 200 from every Group A route, matching
// TestGetRepositoryFreshnessScopedAllowedRepositoryReturnsData.
func TestGroupARepositoryRoutesScopedAllowedRepositoryReturnsData(t *testing.T) {
	t.Parallel()

	for _, tc := range groupARepositoryRoutes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := groupARepositoryTestHandler()
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
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
		})
	}
}

// TestGroupARepositoryRoutesScopedDeniedRepositoryReturns404 verifies a scoped
// caller with NO grant on the resolved repository id gets a 404, never a 403
// that would confirm the repository's existence, matching
// TestGetRepositoryFreshnessScopedDeniedRepositoryReturns404.
func TestGroupARepositoryRoutesScopedDeniedRepositoryReturns404(t *testing.T) {
	t.Parallel()

	for _, tc := range groupARepositoryRoutes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := groupARepositoryTestHandler()
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
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
		})
	}
}

// TestAuthMiddlewareWithScopedTokensAllowsGroupARepositoryRoutes is the #5167
// analogue of TestAuthMiddlewareWithScopedTokensAllowsRepositoryFreshnessRoute
// (itself the PR #5150 review regression): it routes a real scoped bearer
// token through AuthMiddlewareWithScopedTokens, not a bare mux, so it would
// catch the exact #5150 failure shape (an advertised-but-unwired route 403ing
// every scoped caller before the handler's own grant filtering ever runs) for
// each Group A route.
func TestAuthMiddlewareWithScopedTokensAllowsGroupARepositoryRoutes(t *testing.T) {
	t.Parallel()

	newMiddlewareWrappedHandler := func(allowedRepositoryIDs []string) http.Handler {
		handler := groupARepositoryTestHandler()
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

	for _, tc := range groupARepositoryRoutes {
		t.Run(tc.name+"/grant reaches the handler", func(t *testing.T) {
			t.Parallel()

			middleware := newMiddlewareWrappedHandler([]string{"repo-1"})
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
			}
		})

		t.Run(tc.name+"/no grant is a 404, not a 403", func(t *testing.T) {
			t.Parallel()

			middleware := newMiddlewareWrappedHandler([]string{"repo-other"})
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusNotFound; got != want {
				t.Fatalf("status = %d, want %d (grant filtering, not middleware 403); body = %s", got, want, w.Body.String())
			}
		})
	}
}

// TestAuthMiddlewareWithScopedTokensAllowsGroupARepositoryOrgRepoSlug is the
// #5167 PR #5324 review regression (P1): a scoped caller addressing a Group A
// route by an org/repo-style selector must reach the grant-filtering handler,
// not be 403'd by the allowlist matcher. The MCP dispatchers build the path
// with url.PathEscape (github.com/eshu-hq/eshu/go/internal/mcp/
// dispatch_repositories.go), so an "org/order-service" selector arrives as the
// single escaped segment "org%2Forder-service". http.NewRequest decodes
// r.URL.Path back to "org/order-service" (a slash reappears) but preserves the
// escaped form in r.URL.RawPath/EscapedPath(); scopedRepositorySingleResourceRoute
// therefore MUST match on the escaped path so its slash guard sees one segment,
// not two. The slash-free-only Group A tests above miss this because "repo-1"
// escapes to itself.
//
// order-service's canonical id is repo-1 (repositoryStatsCatalogEntry's
// RepoSlug is "org/order-service"), so a caller granted repo-1 resolves and
// reads it, while a caller granted only repo-other gets the same not-found
// (404) parity as every other cross-tenant selector -- never a 403 that would
// confirm the repository's existence.
func TestAuthMiddlewareWithScopedTokensAllowsGroupARepositoryOrgRepoSlug(t *testing.T) {
	t.Parallel()

	const orgRepoSelector = "org/order-service" // repositoryStatsCatalogEntry().RepoSlug; resolves to repo-1

	newMiddlewareWrappedHandler := func(allowedRepositoryIDs []string) http.Handler {
		handler := groupARepositoryTestHandler()
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

	for _, tc := range groupARepositoryRoutes {
		// Build the path exactly as the MCP dispatcher does: PathEscape the
		// selector so "org/order-service" becomes the single segment
		// "org%2Forder-service".
		escapedPath := "/api/v0/repositories/" + url.PathEscape(orgRepoSelector) + "/" + tc.name

		t.Run(tc.name+"/granted org-repo slug reaches the handler", func(t *testing.T) {
			t.Parallel()

			middleware := newMiddlewareWrappedHandler([]string{"repo-1"})
			req := httptest.NewRequest(http.MethodGet, escapedPath, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller using an org/repo slug); path = %s; body = %s", got, want, escapedPath, w.Body.String())
			}
		})

		t.Run(tc.name+"/ungranted org-repo slug is a 404, not a 403", func(t *testing.T) {
			t.Parallel()

			middleware := newMiddlewareWrappedHandler([]string{"repo-other"})
			req := httptest.NewRequest(http.MethodGet, escapedPath, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusNotFound; got != want {
				t.Fatalf("status = %d, want %d (grant filtering, not middleware 403); path = %s; body = %s", got, want, escapedPath, w.Body.String())
			}
		})
	}
}
