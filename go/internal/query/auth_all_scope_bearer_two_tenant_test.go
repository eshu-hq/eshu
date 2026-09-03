// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAllScopeBearerTwoTenantBoundary is the data-plane half of #6450's
// residual item 1, and it is the assertion the middleware tables above cannot
// make. They prove the status code; this one proves the read.
//
// The distinction matters because the defect was never the status code. An
// all-scope bearer's RepositoryAccessFilter has AllScopes set, so Scoped() is
// false, so $3 in resolveChangedSinceScopeQuery and $8 in
// listGenerationLifecycleQuery short-circuit and both routes answer from the
// whole corpus -- every tenant's rows, in a deployment that has deliberately
// left BrowserSessionRoutePolicy at its fail-closed zero value. A test that
// only asserted 403 would still pass if a later refactor moved the refusal
// after the handler had already run its query, or wired a second admission
// path around it. These cases assert on the store fake: under
// hosted_multi_tenant the changed-since reader is never called at all, so
// there is no cross-tenant read to leak, whatever the response says.
//
// It runs the two promoted routes against the same two-tenant fixtures their
// own grant-boundary proofs use (grantMirroringChangedSince and
// grantMirroringGenerations, which apply the shipped SQL predicate rather
// than merely recording the filter), so "the read is gone" is measured
// against the same corpus "the grant binds" is measured against.
func TestAllScopeBearerTwoTenantBoundary(t *testing.T) {
	t.Parallel()

	allScopeBearer := AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		AllScopes:   true,
	}

	t.Run("changed-since", func(t *testing.T) {
		t.Parallel()

		t.Run("hosted multi tenant never runs the read", func(t *testing.T) {
			t.Parallel()

			rec, reader := serveChangedSinceThroughBearerMiddleware(
				t, allScopeBearer, ScopedRoutePolicyForGovernanceMode(GovernanceStatusConfig{Mode: "hosted_multi_tenant"}), "repo-b",
			)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			// The assertion the status code cannot make. Before residual item
			// 1 closed, this was a 200 carrying tenant B's delta; a refusal
			// that still consulted the store would be a refusal that had
			// already done the cross-tenant read.
			if reader.called {
				t.Fatalf("the changed-since reader ran for a refused all-scope bearer; filter = %#v", reader.lastFilter)
			}
			// Belt and braces on the same point: even the other tenant's scope
			// id must not appear in the refusal body.
			for _, leak := range []string{"scope-a", "scope-b"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("the refusal body carries a scope identity %q: %s", leak, rec.Body.String())
				}
			}
		})

		t.Run("a local deployment still answers, unbounded and on purpose", func(t *testing.T) {
			t.Parallel()

			// The honest other half. local_no_policy is what a laptop and a
			// single-tenant install run, and there an admin token reading the
			// whole corpus is the documented behaviour, not a leak -- there is
			// one tenant. Pinning it here keeps the refusal above from being
			// read as "all-scope bearers lost this route", and it is what
			// would fail if a future change made the policy fail-closed
			// everywhere.
			rec, reader := serveChangedSinceThroughBearerMiddleware(
				t, allScopeBearer, ScopedRoutePolicyForGovernanceMode(GovernanceStatusConfig{Mode: "local_no_policy"}), "repo-b",
			)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if !reader.called {
				t.Fatal("the changed-since reader was never called; an admitted caller must reach the query")
			}
			if reader.lastFilter.Scoped {
				t.Fatal("filter.Scoped = true for an all-scope bearer; the grant predicate is inert for it, which is exactly why hosted_multi_tenant refuses it")
			}
			data, _ := decodeChangedSinceEnvelope(t, rec)
			if got, want := data["scope_id"], "scope-b"; got != want {
				t.Fatalf("data[scope_id] = %v, want %q; the unbounded read resolves the other tenant's scope, which is the posture local_no_policy accepts", got, want)
			}
		})

		t.Run("a restricted bearer is unchanged in both modes", func(t *testing.T) {
			t.Parallel()

			// The population the fix must not have touched. A token carrying
			// real ids has a grant the query binds, so it is admitted under
			// either policy and bounded by that grant either way.
			for _, mode := range []string{"hosted_multi_tenant", "local_no_policy"} {
				mode := mode
				t.Run(mode, func(t *testing.T) {
					t.Parallel()

					rec, reader := serveChangedSinceThroughBearerMiddleware(
						t, scopedChangedSinceTenantA(), ScopedRoutePolicyForGovernanceMode(GovernanceStatusConfig{Mode: mode}), "repo-a",
					)

					if rec.Code != http.StatusOK {
						t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
					}
					if !reader.lastFilter.Scoped {
						t.Fatal("filter.Scoped = false for a restricted bearer; its grant must still bind")
					}
					data, _ := decodeChangedSinceEnvelope(t, rec)
					if got, want := data["scope_id"], "scope-a"; got != want {
						t.Fatalf("data[scope_id] = %v, want %q", got, want)
					}
				})
			}
		})
	})

	t.Run("generations", func(t *testing.T) {
		t.Parallel()

		t.Run("hosted multi tenant never runs the read", func(t *testing.T) {
			t.Parallel()

			rec, reader := serveGenerationsThroughBearerMiddleware(
				t, allScopeBearer, ScopedRoutePolicyForGovernanceMode(GovernanceStatusConfig{Mode: "hosted_multi_tenant"}), "gen-b",
			)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			if reader.called {
				t.Fatalf("the generation lifecycle reader ran for a refused all-scope bearer; filter = %#v", reader.lastFilter)
			}
			for _, leak := range []string{"gen-a", "gen-b", "scope-b"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("the refusal body carries a generation or scope identity %q: %s", leak, rec.Body.String())
				}
			}
		})

		t.Run("a local deployment still answers, unbounded and on purpose", func(t *testing.T) {
			t.Parallel()

			rec, reader := serveGenerationsThroughBearerMiddleware(
				t, allScopeBearer, ScopedRoutePolicyForGovernanceMode(GovernanceStatusConfig{Mode: "local_no_policy"}), "gen-b",
			)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if !reader.called {
				t.Fatal("the generation lifecycle reader was never called; an admitted caller must reach the query")
			}
			if reader.lastFilter.Scoped {
				t.Fatal("filter.Scoped = true for an all-scope bearer; the grant predicate is inert for it")
			}
		})
	})
}

// serveChangedSinceThroughBearerMiddleware drives one bearer read of GET
// /api/v0/freshness/changed-since through the REAL auth middleware into the
// real FreshnessHandler, and hands back both the response and the store fake.
//
// Going through the middleware is the whole point. The sibling
// TestChangedSinceTwoTenantGrantBoundary mounts the handler directly with an
// AuthContext already in the request context, which is right for proving the
// SQL grant binds but cannot see an admission decision at all. Here the
// credential arrives as an Authorization header and the middleware resolves
// it, so a refusal is proven to happen before the handler, not merely
// alongside it.
func serveChangedSinceThroughBearerMiddleware(
	t *testing.T,
	auth AuthContext,
	policy BrowserSessionRoutePolicy,
	repository string,
) (*httptest.ResponseRecorder, *grantMirroringChangedSince) {
	t.Helper()

	reader := &grantMirroringChangedSince{scopes: twoTenantChangedSinceScopes()}
	mux := http.NewServeMux()
	(&FreshnessHandler{ChangedSince: reader, Profile: ProfileLocalAuthoritative}).Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/changed-since?repository="+repository+
			"&since_generation_id="+changedSinceTwoTenantPriorGeneration,
		nil,
	)
	return serveThroughBearerMiddleware(t, req, auth, policy, mux), reader
}

// serveGenerationsThroughBearerMiddleware is the sibling for GET
// /api/v0/freshness/generations. See its twin above for why the request goes
// through the middleware rather than straight into the handler.
func serveGenerationsThroughBearerMiddleware(
	t *testing.T,
	auth AuthContext,
	policy BrowserSessionRoutePolicy,
	generationID string,
) (*httptest.ResponseRecorder, *grantMirroringGenerations) {
	t.Helper()

	reader := &grantMirroringGenerations{rows: twoTenantGenerationRows()}
	mux := http.NewServeMux()
	(&FreshnessHandler{Generations: reader, Profile: ProfileLocalAuthoritative}).Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/generations?generation_id="+generationID,
		nil,
	)
	return serveThroughBearerMiddleware(t, req, auth, policy, mux), reader
}

// serveThroughBearerMiddleware wraps next in the same route-policy-carrying
// middleware cmd/mcp-server wires and serves one bearer request through it.
func serveThroughBearerMiddleware(
	t *testing.T,
	req *http.Request,
	auth AuthContext,
	policy BrowserSessionRoutePolicy,
	next http.Handler,
) *httptest.ResponseRecorder {
	t.Helper()

	handler := AuthMiddlewareWithScopedTokensAndRoutePolicy(
		"", &fakeScopedTokenResolver{context: auth, ok: true}, next, policy,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
