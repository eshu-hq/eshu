// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// fakeScopedResolver resolves exactly one bearer token to a scoped AuthContext;
// any other token is unrecognized (so the shared-token path is taken).
type fakeScopedResolver struct{ scopedToken string }

func (f fakeScopedResolver) ResolveScopedToken(_ context.Context, token string) (query.AuthContext, bool, error) {
	if token == f.scopedToken {
		return query.AuthContext{Mode: query.AuthModeScoped}, true, nil
	}
	return query.AuthContext{}, false, nil
}

// TestMCPRunner_ScopedCaller_CannotReachNonScopedRoute is the cross-tenant leak
// regression test for scoped Ask (#3300). It proves that when the in-process
// runner dispatches through the scoped-auth-wrapped handler under a scoped
// caller's token, a tool mapped to a route OUTSIDE the scoped allowlist is
// blocked by the scoped-route gate BEFORE the inner handler runs — so no
// cross-scope data can leak through Ask. Allowlisted routes and the
// shared-admin path remain reachable.
//
// The non-allowlisted probe is execute_cypher_query → POST /api/v0/code/cypher,
// a PERMANENTLY shared-key-only route (#5167 Group C, sharedKeyOnlyRoutes): its
// handler runs the caller's literal Cypher with no grant to bind against, so it
// can never join scopedHTTPRouteSupportsTenantFilter and this assertion stays
// valid for the whole #5167 epic. Do NOT repoint this to a pendingRowFilteringRoutes
// entry (e.g. the get_ecosystem_overview → /api/v0/ecosystem/overview route this
// test used before #5167 F-6 W6): every pending route is destined for scoped
// promotion (the ledger draining to empty is the epic's exit criterion), which
// would flip this control's expected denial to a reachable 200 — exactly the W6
// regression this replaced when W6 promoted ecosystem/overview.
func TestMCPRunner_ScopedCaller_CannotReachNonScopedRoute(t *testing.T) {
	t.Parallel()

	const adminKey = "admin-secret"
	const scopedTok = "scoped-tok"

	var cypherReached, reposReached bool
	inner := http.NewServeMux()
	inner.HandleFunc("/api/v0/code/cypher", func(w http.ResponseWriter, _ *http.Request) {
		cypherReached = true
		_, _ = w.Write([]byte(`{"rows":[{"whole_graph_total":9999}]}`))
	})
	inner.HandleFunc("/api/v0/repositories", func(w http.ResponseWriter, _ *http.Request) {
		reposReached = true
		_, _ = w.Write([]byte(`{"repositories":[]}`))
	})

	authed := query.AuthMiddlewareWithScopedTokens(adminKey, fakeScopedResolver{scopedToken: scopedTok}, inner)
	runner := NewMCPRunner(authed, "Bearer "+adminKey, nil)

	// Scoped caller → non-allowlisted route: must be denied by the gate before
	// the inner handler executes. This is the leak that must stay closed.
	scopedCtx := ContextWithCallerAuthHeader(context.Background(), "Bearer "+scopedTok)
	if _, err := runner.Run(scopedCtx, "execute_cypher_query", nil); err != nil {
		t.Logf("scoped non-allowlisted dispatch returned err (expected denial path): %v", err)
	}
	if cypherReached {
		t.Fatal("scoped caller reached POST /api/v0/code/cypher through Ask — cross-scope leak")
	}

	// Scoped caller → allowlisted route: reaches the handler (scoped data).
	if _, err := runner.Run(scopedCtx, "list_indexed_repositories", nil); err != nil {
		t.Fatalf("scoped allowlisted route errored: %v", err)
	}
	if !reposReached {
		t.Error("scoped caller did not reach the allowlisted /api/v0/repositories route")
	}

	// Shared-admin caller (no caller header → baked-in admin token): full access,
	// including the non-allowlisted route, exactly as before scoped support.
	cypherReached = false
	if _, err := runner.Run(context.Background(), "execute_cypher_query", nil); err != nil {
		t.Fatalf("shared-admin route errored: %v", err)
	}
	if !cypherReached {
		t.Error("shared-admin caller did not reach the shared-key-only cypher route")
	}
}
