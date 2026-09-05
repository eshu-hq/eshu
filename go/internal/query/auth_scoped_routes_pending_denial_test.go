// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuthMiddlewareRejectsScopedCallersOnPendingRowFilteringRoutes locks
// the #5167 Group B fail-closed contract at the middleware layer: every
// route on the pending-row-filtering ledger refuses a scoped
// (personal-token) caller with 403 before any handler runs, so no handler
// below can serve unfiltered rows to a grant-bound caller. The test is
// ledger-driven -- promoting a route out of pendingRowFilteringRoutes moves
// it out of this test automatically, and adding a route to the ledger moves
// it in -- so the ledger cannot silently grow a served-instead-of-denied
// entry. The next handler is a stub that records admission: denial must
// happen in AuthMiddlewareWithScopedTokens, not in handler code, because
// several Group B handlers also validate selectors against the grant as
// defense-in-depth (e.g. call-chain's endpoint check), and that layer must
// never be the only thing standing between a scoped caller and unfiltered
// rows.
func TestAuthMiddlewareRejectsScopedCallersOnPendingRowFilteringRoutes(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant_a",
			WorkspaceID:          "workspace_a",
			AllowedRepositoryIDs: []string{"repo_a"},
		},
		ok: true,
	}

	for surface := range pendingRowFilteringRoutes {
		surface := surface
		method, path, ok := strings.Cut(surface, " ")
		if !ok || method == "" || path == "" {
			t.Fatalf("pendingRowFilteringRoutes key %q is not \"METHOD /path\"", surface)
		}
		t.Run(method+" "+path, func(t *testing.T) {
			t.Parallel()

			called := false
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(method, path, nil)
			req.Header.Set("Authorization", "Bearer [REDACTED]")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if called {
				t.Fatalf("next handler called for Group B route %s -- scoped denial must happen in middleware", surface)
			}
			if got, want := rec.Code, http.StatusForbidden; got != want {
				t.Fatalf("%s status = %d, want %d; body = %s", surface, got, want, rec.Body.String())
			}
		})
	}
}
