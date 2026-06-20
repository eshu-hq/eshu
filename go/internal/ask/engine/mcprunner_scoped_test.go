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
// caller's token, a tool mapped to a route OUTSIDE the scoped allowlist (e.g.
// get_ecosystem_overview → GET /api/v0/ecosystem/overview, whole-graph counts
// that ignore AuthContext) is blocked by the scoped-route gate BEFORE the inner
// handler runs — so no cross-scope data can leak through Ask. Allowlisted routes
// and the shared-admin path remain reachable.
func TestMCPRunner_ScopedCaller_CannotReachNonScopedRoute(t *testing.T) {
	t.Parallel()

	const adminKey = "admin-secret"
	const scopedTok = "scoped-tok"

	var ecosystemReached, reposReached bool
	inner := http.NewServeMux()
	inner.HandleFunc("/api/v0/ecosystem/overview", func(w http.ResponseWriter, _ *http.Request) {
		ecosystemReached = true
		_, _ = w.Write([]byte(`{"whole_graph_total":9999}`))
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
	if _, err := runner.Run(scopedCtx, "get_ecosystem_overview", nil); err != nil {
		t.Logf("scoped non-allowlisted dispatch returned err (expected denial path): %v", err)
	}
	if ecosystemReached {
		t.Fatal("scoped caller reached GET /api/v0/ecosystem/overview through Ask — cross-scope leak")
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
	ecosystemReached = false
	if _, err := runner.Run(context.Background(), "get_ecosystem_overview", nil); err != nil {
		t.Fatalf("shared-admin route errored: %v", err)
	}
	if !ecosystemReached {
		t.Error("shared-admin caller did not reach the ecosystem route")
	}
}
