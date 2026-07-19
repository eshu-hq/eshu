// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// iacResourceRepoNode is iacResourceNode plus a durable repo_id, the field the
// scoped predicate binds against.
func iacResourceRepoNode(id, name, resourceType, provider, repoID string) map[string]any {
	node := iacResourceNode(id, name, resourceType, provider)
	node["repo_id"] = repoID
	return node
}

// scopedIaCResourceRequest builds a GET /api/v0/iac/resources request whose
// context carries the given scoped AuthContext, mirroring what
// AuthMiddlewareWithScopedTokens installs before the handler runs.
func scopedIaCResourceRequest(t *testing.T, target string, auth AuthContext) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	return req.WithContext(ContextWithAuthContext(req.Context(), auth))
}

func TestScopedIaCResourceListGateAllowsRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=10", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestScopedIaCResourceListGateRejectsAdjacentRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mockHandler())

	// POST /api/v0/impact/trace-resource-to-code is an adjacent
	// resource-graph route that stays fail-closed for scoped tokens (#5167
	// Group B, pendingRowFilteringRoutes -- it has no AllowedScopeIDs
	// grant-filtering wired yet: the #5167 W3 family flagged it as unsafe to
	// allowlist because its traversal endpoints carry no repo_id property; see
	// auth_scoped_routes_impact.go's doc comment); only the GET list route and
	// the #5167 W4 iac/replatforming family (POST /api/v0/iac/dead among
	// them) are scoped-enabled. (POST /api/v0/aws/runtime-drift/findings,
	// this test's route until the #5167 F-6 W6 cloud/aws family workstream
	// scope-filtered it, is no longer a valid adjacent example -- it now
	// reaches its handler under a scoped token like iac/dead.) iac/dead
	// reaches its handler under a scoped token and enforces the grant there:
	// TestHandleDeadIaCScopedGrantAllowsInGrantRepository serves an in-grant
	// repo, while TestHandleDeadIaCScopedGrantRejectsOutOfGrantRepository and
	// TestHandleDeadIaCScopedEmptyGrantRejectsAnyRepository return 400 at the
	// handler (not a transport-layer 403).
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-resource-to-code", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestScopedIaCResourceListEmptyGrantSkipsGraphRead(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceRepoNode("a1", "aws_s3_bucket.logs", "aws_s3_bucket", "aws", "repo-team-a"),
	}}
	handler := &IaCHandler{Graph: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Scoped token with no granted repository or ingestion scope: must return a
	// bounded empty page without reading the graph.
	req := scopedIaCResourceRequest(t, "/api/v0/iac/resources?limit=10", AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if graph.calls != 0 {
		t.Fatalf("graph.calls = %d, want 0 (empty-grant must not read the graph)", graph.calls)
	}
	body := decodeIaCResourceList(t, w)
	if body.Count != 0 || len(body.Resources) != 0 {
		t.Fatalf("count = %d, resources = %d, want 0", body.Count, len(body.Resources))
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false")
	}
	if body.Limit != 10 {
		t.Fatalf("limit = %d, want 10", body.Limit)
	}
}

func TestScopedIaCResourceListBindsRepoPredicateAndParams(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceRepoNode("a1", "aws_s3_bucket.logs", "aws_s3_bucket", "aws", "repo-team-a"),
	}}
	handler := &IaCHandler{Graph: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := scopedIaCResourceRequest(t, "/api/v0/iac/resources?limit=10", AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if graph.calls != 1 {
		t.Fatalf("graph.calls = %d, want 1", graph.calls)
	}
	// The repo-anchored predicate must render in scoped mode.
	if !strings.Contains(graph.lastCypher, "n.repo_id IN $allowed_repository_ids") ||
		!strings.Contains(graph.lastCypher, "n.repo_id IN $allowed_scope_ids") {
		t.Fatalf("cypher missing scoped repo predicate: %s", graph.lastCypher)
	}
	// The predicate must sit in the WHERE chain, before ORDER BY / LIMIT.
	whereIdx := strings.Index(graph.lastCypher, "n.repo_id IN $allowed_repository_ids")
	orderIdx := strings.Index(graph.lastCypher, "ORDER BY")
	if whereIdx < 0 || orderIdx < 0 || whereIdx > orderIdx {
		t.Fatalf("scope predicate must precede ORDER BY: %s", graph.lastCypher)
	}
	repos, ok := graph.lastParams["allowed_repository_ids"].([]string)
	if !ok || len(repos) != 1 || repos[0] != "repo-team-a" {
		t.Fatalf("allowed_repository_ids param = %#v, want [repo-team-a]", graph.lastParams["allowed_repository_ids"])
	}
	scopes, ok := graph.lastParams["allowed_scope_ids"].([]string)
	if !ok || len(scopes) != 1 || scopes[0] != "scope-a" {
		t.Fatalf("allowed_scope_ids param = %#v, want [scope-a]", graph.lastParams["allowed_scope_ids"])
	}
}

func TestScopedIaCResourceListUnscopedQueryUnchanged(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceRepoNode("a1", "aws_s3_bucket.logs", "aws_s3_bucket", "aws", "repo-team-a"),
	}}
	handler := &IaCHandler{Graph: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// No AuthContext in the request context: shared / admin / local caller. The
	// query and params must be byte-identical to the pre-scoped read (no scope
	// predicate, no grant params).
	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if strings.Contains(graph.lastCypher, "allowed_repository_ids") ||
		strings.Contains(graph.lastCypher, "allowed_scope_ids") {
		t.Fatalf("unscoped cypher must not contain scope predicate: %s", graph.lastCypher)
	}
	if _, ok := graph.lastParams["allowed_repository_ids"]; ok {
		t.Fatalf("unscoped params must not bind allowed_repository_ids: %#v", graph.lastParams)
	}
	if _, ok := graph.lastParams["allowed_scope_ids"]; ok {
		t.Fatalf("unscoped params must not bind allowed_scope_ids: %#v", graph.lastParams)
	}
}
