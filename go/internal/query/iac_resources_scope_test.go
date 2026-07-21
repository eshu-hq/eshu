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
	node["generation_id"] = "generation-active"
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

func TestScopedIaCResourceListGateRejectsSharedKeyOnlyRoute(t *testing.T) {
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

	// The negative control deliberately targets a PERMANENTLY shared-key-only
	// route -- POST /api/v0/code/cypher (#5167 Group C, sharedKeyOnlyRoutes,
	// auth_scoped_routes_shared_key_only.go) -- so this transport-layer 403
	// assertion survives the whole #5167 epic. A scoped token on any route
	// outside scopedHTTPRouteSupportsTenantFilter gets scopedRouteDeniedResponse
	// (auth.go: the auth.Mode == AuthModeScoped && !scopedHTTPRouteSupportsTenantFilter
	// branch), and /code/cypher can never join that allowlist: its handler runs
	// the caller's literal Cypher with no selector to intersect against a grant,
	// so it is excluded by design, not pending.
	//
	// Do NOT repoint this to a pendingRowFilteringRoutes entry: every route in
	// that ledger is destined for scoped promotion by some F-6 family (the
	// ledger draining to empty IS the epic's exit criterion), so a pending
	// route would flip this control's expected 403 to 200 the moment its family
	// lands -- exactly the W6 regression this replaced (aws/runtime-drift/findings
	// was pending when W4 authored this test, then W6 promoted it and enforced
	// the grant at its own handler via TestHandleAWSRuntimeDriftFindingsScoped*).
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/cypher", nil)
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
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Scoped token with no granted repository or ingestion scope: must return a
	// bounded empty page without reading the graph.
	req := scopedIaCResourceRequest(t, "/api/v0/iac/resources?limit=10&include_facets=true", AuthContext{
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
	if !strings.Contains(w.Body.String(), `"summary":{"total":0`) {
		t.Fatalf("empty-grant response missing authoritative zero summary: %s", w.Body.String())
	}
}

func TestScopedIaCResourceListBindsRepoPredicateAndParams(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceRepoNode("a1", "aws_s3_bucket.logs", "aws_s3_bucket", "aws", "repo-team-a"),
	}}
	handler := newIaCResourceTestHandler(graph)
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
	inventory := handler.Inventory.(*stubIaCInventoryStore)
	if got := inventory.lastAccess.grantedRepositoryIDs(); len(got) != 1 || got[0] != "repo-team-a" {
		t.Fatalf("inventory repository grants = %#v, want [repo-team-a]", got)
	}
	if got := inventory.lastAccess.grantedScopeIDs(); len(got) != 1 || got[0] != "scope-a" {
		t.Fatalf("inventory scope grants = %#v, want [scope-a]", got)
	}
	if strings.Contains(graph.lastCypher, "allowed_repository_ids") || strings.Contains(graph.lastCypher, "allowed_scope_ids") {
		t.Fatalf("graph hydration must be bounded only by already-authorized candidate ids: %s", graph.lastCypher)
	}
}

func TestScopedIaCResourceListUnscopedQueryUnchanged(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceRepoNode("a1", "aws_s3_bucket.logs", "aws_s3_bucket", "aws", "repo-team-a"),
	}}
	handler := newIaCResourceTestHandler(graph)
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
