// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// errorFlowKinds are the failure.Case kinds a step whose store, service, or
// generator error reaches the answer must handle: a backend fault, a client
// cancel of the request, and an inner cancel on a live request (#7674). The
// auth and session routes read and write through the writer store, never the
// guarded PostgreSQL reader, so the reader-fence case never applies.
var errorFlowKinds = []failure.Kind{failure.Fault, failure.ClientCancel, failure.InnerCancel}

// faultOnlyKinds is the kind a step handles when no request-context error can
// reach it: it answers a package sentinel for an empty value, or a render or
// entropy error that never sees the request context. Only the server fault
// case applies; a cancel cannot reach it.
var faultOnlyKinds = []failure.Kind{failure.Fault}

// runAuthFailureSteps drives every route of suite through each case whose
// kind is in kinds, one subtest per route and case, and fails each subtest
// with every assertion its answer breaks.
func runAuthFailureSteps(t *testing.T, suite failure.Suite, kinds []failure.Kind) {
	t.Helper()
	for _, route := range suite.Routes {
		for _, tc := range failure.Cases() {
			if !slices.Contains(kinds, tc.Kind) {
				continue
			}
			t.Run(route.Name+"/"+tc.Name, func(t *testing.T) {
				if err := failure.Check(suite, route, tc); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

// withHeaders mounts inner on its own mux behind a catch-all route that sets
// headers on each request before that mux serves it. failure.Serve sets only
// Accept, but the SAML ACS route parses its form body (Content-Type) and the
// logout and workspace-switch routes hash the session cookie (Cookie).
type withHeaders struct {
	inner   failure.Mounter
	headers map[string]string
}

// Mount registers the catch-all route on mux.
func (h withHeaders) Mount(mux *http.ServeMux) {
	inner := http.NewServeMux()
	h.inner.Mount(inner)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		for key, value := range h.headers {
			r.Header.Set(key, value)
		}
		inner.ServeHTTP(w, r)
	})
}

// withSessionCookie sends each request to inner with a browser session cookie.
func withSessionCookie(inner failure.Mounter) failure.Mounter {
	return withHeaders{inner: inner, headers: map[string]string{"Cookie": BrowserSessionCookieName + "=session-secret"}}
}

// browserSessionScope is a browser-session caller bound to a tenant and
// workspace, the caller the session routes accept.
func browserSessionScope(ctx context.Context) context.Context {
	return ContextWithAuthContext(ctx, AuthContext{
		Mode: AuthModeBrowserSession, TenantID: "tenant_a", WorkspaceID: "workspace_a",
		SubjectIDHash: "sha256:subject", AllScopes: true,
	})
}

// bearerScope is an explicit API credential with a tenant and workspace, the
// caller POST /api/v0/auth/browser-session upgrades to a session.
func bearerScope(ctx context.Context) context.Context {
	return ContextWithAuthContext(ctx, AuthContext{
		Mode: AuthModeShared, TenantID: "tenant_a", WorkspaceID: "workspace_a", AllScopes: true,
	})
}

// failingSecret is a NewSecret double that fails on call number failOn
// (1-based) with err and returns a fresh value on every other call.
func failingSecret(failOn int, err error) func() (string, error) {
	calls := 0
	return func() (string, error) {
		calls++
		if calls == failOn {
			return "", err
		}
		return fmt.Sprintf("secret-%d", calls), nil
	}
}
