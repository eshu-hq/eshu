// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/scopedtoken"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// This suite builds the cmd/api auth middleware EXACTLY as wireAPI does: the
// real always-wired Postgres identity resolver, a real
// scopedtoken.ChainResolvers(identity, oidc, file) chain, an always-wired
// browser-session resolver, the real authEnforcementConfigured predicate, and
// the real production glue wrapAPIAuth (which threads the predicate into
// AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement).
// The identity and session resolvers are always non-nil in production, which is
// why the enforcement gate is a wiring-time predicate and not a resolver-presence
// check (the broken first attempt).

// stubExecQueryer is an empty identity-token store; see the mcp-server suite's
// identical type for the rationale (returns no rows so the identity resolver
// reports not-found and the chain falls through).
type stubExecQueryer struct{}

func (stubExecQueryer) QueryContext(context.Context, string, ...any) (pgstatus.Rows, error) {
	return emptyRows{}, nil
}

func (stubExecQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("stubExecQueryer: unexpected exec")
}

type emptyRows struct{}

func (emptyRows) Next() bool        { return false }
func (emptyRows) Scan(...any) error { return errors.New("emptyRows: no rows") }
func (emptyRows) Err() error        { return nil }
func (emptyRows) Close() error      { return nil }

// recordingScopedResolver represents a configured-but-not-matching scoped
// resolver (e.g. the OIDC bearer resolver). It records whether it was consulted.
type recordingScopedResolver struct{ called bool }

func (r *recordingScopedResolver) ResolveScopedToken(context.Context, string) (query.AuthContext, bool, error) {
	r.called = true
	return query.AuthContext{}, false, nil
}

// fakeSessionResolver stands in for the always-wired browser-session resolver.
// It authenticates any presented session cookie to a tenant-bound all-scopes
// owner session, and records whether it was called (so tests can assert a
// cookieless request never reaches it).
type fakeSessionResolver struct{ called bool }

func (f *fakeSessionResolver) ResolveBrowserSession(
	_ context.Context,
	_ string,
	_ string,
	_ bool,
	_ time.Time,
) (query.AuthContext, bool, error) {
	f.called = true
	return query.AuthContext{
		Mode:        query.AuthModeBrowserSession,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}, true, nil
}

// buildAPIAuthHandler assembles the middleware the way cmd/api/wiring.go does,
// through the real wrapAPIAuth glue.
func buildAPIAuthHandler(
	apiKey string,
	fileResolver, oidcResolver query.ScopedTokenResolver,
	session query.BrowserSessionResolver,
) http.Handler {
	identityResolver := scopedtoken.NewPostgresIdentityResolver(
		pgstatus.NewScopedAPITokenStore(stubExecQueryer{}),
	)
	enforcement := authEnforcementConfigured(apiKey, fileResolver, oidcResolver)
	scopedTokenResolver := scopedtoken.ChainResolvers(identityResolver, oidcResolver, fileResolver)
	return wrapAPIAuth(
		apiKey,
		scopedTokenResolver,
		session,
		okHandler(),
		nil,
		query.GovernanceStatusConfig{},
		enforcement,
	)
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func writeScopedTokenFile(t *testing.T, token string) query.ScopedTokenResolver {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	doc := map[string]any{
		"version": 1,
		"tokens": []map[string]any{{
			"token_sha256": hex.EncodeToString(sum[:]),
			"tenant_id":    "tenant_a",
			"workspace_id": "workspace_a",
			"all_scopes":   true,
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	path := filepath.Join(t.TempDir(), "scoped-tokens.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	resolver, err := scopedtoken.LoadRegistryFromFile(path)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return resolver
}

func TestAPIAuthDemoConfigServesHeaderlessOpen(t *testing.T) {
	t.Parallel()

	// Demo: no explicit knobs, but identity + session resolvers present (always).
	session := &fakeSessionResolver{}
	handler := buildAPIAuthHandler("", nil, nil, session)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("headerless status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if session.called {
		t.Fatal("session resolver consulted for a cookieless headerless request")
	}
}

func TestAPIAuthOIDCConfiguredDeniesHeaderless(t *testing.T) {
	t.Parallel()

	oidc := &recordingScopedResolver{}
	handler := buildAPIAuthHandler("", nil, oidc, &fakeSessionResolver{})

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("headerless status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if oidc.called {
		t.Fatal("oidc resolver consulted for a headerless request; denial must precede resolution")
	}
}

func TestAPIAuthTokenFileConfiguredDeniesHeaderlessAdmitsToken(t *testing.T) {
	t.Parallel()

	const token = "file-registry-token-secret"
	fileResolver := writeScopedTokenFile(t, token)
	handler := buildAPIAuthHandler("", fileResolver, nil, &fakeSessionResolver{})

	headerless := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	headerlessRec := httptest.NewRecorder()
	handler.ServeHTTP(headerlessRec, headerless)
	if got, want := headerlessRec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("headerless status = %d, want %d; body = %s", got, want, headerlessRec.Body.String())
	}

	authed := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	authed.Header.Set("Authorization", "Bearer "+token)
	authedRec := httptest.NewRecorder()
	handler.ServeHTTP(authedRec, authed)
	if got, want := authedRec.Code, http.StatusOK; got != want {
		t.Fatalf("valid file token status = %d, want %d; body = %s", got, want, authedRec.Body.String())
	}
}

func TestAPIAuthSharedKeyDeniesHeaderlessAdmitsKey(t *testing.T) {
	t.Parallel()

	const key = "shared-api-key"
	handler := buildAPIAuthHandler(key, nil, nil, &fakeSessionResolver{})

	headerless := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	headerlessRec := httptest.NewRecorder()
	handler.ServeHTTP(headerlessRec, headerless)
	if got, want := headerlessRec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("headerless status = %d, want %d", got, want)
	}

	authed := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	authed.Header.Set("Authorization", "Bearer "+key)
	authedRec := httptest.NewRecorder()
	handler.ServeHTTP(authedRec, authed)
	if got, want := authedRec.Code, http.StatusOK; got != want {
		t.Fatalf("shared key status = %d, want %d", got, want)
	}
}

// TestAPIAuthBootstrapIdentityOnlyServesHeaderlessOpen proves the
// bootstrap-identity-only case (#4962/#4963 seeded identities, NONE of the
// three explicit credential knobs) genuinely, not by coincidence with the
// no-identity demo case. TestAPIAuthDemoConfigServesHeaderlessOpen's stub
// store returns not-found for every credential, so it cannot tell "the
// identity resolver has real data and would authenticate a presented
// credential" apart from "the identity resolver has nothing at all". This
// test seeds ONE real identity-token row through the full production
// resolution pipeline (seededIdentityQueryer) so the distinction is real: the
// bootstrap identity resolves a MATCHING credential to 200, rejects a
// non-matching one with 401 (proving it is not a blanket pass-through), and
// -- the actual ruled behavior -- still serves a HEADERLESS request open,
// because authEnforcementConfigured deliberately excludes the always-wired
// identity resolver as a signal (the demo seeds identities by default).
func TestAPIAuthBootstrapIdentityOnlyServesHeaderlessOpen(t *testing.T) {
	t.Parallel()

	const bootstrapToken = "bootstrap-identity-token-secret"
	seeded := seededIdentityQueryer{
		tokenHash:          pgstatus.ScopedAPITokenHash(bootstrapToken),
		tenantID:           "tenant_bootstrap",
		workspaceID:        "workspace_bootstrap",
		subjectIDHash:      "sha256:bootstrap-subject",
		policyRevisionHash: "policy-rev-bootstrap",
		roleID:             "role-bootstrap-owner",
	}
	identityResolver := scopedtoken.NewPostgresIdentityResolver(pgstatus.NewScopedAPITokenStore(seeded))
	enforcement := authEnforcementConfigured("", nil, nil)
	scopedTokenResolver := scopedtoken.ChainResolvers(identityResolver, nil, nil)
	handler := wrapAPIAuth(
		"", scopedTokenResolver, &fakeSessionResolver{}, okHandler(), nil, query.GovernanceStatusConfig{}, enforcement,
	)

	headerless := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	headerlessRec := httptest.NewRecorder()
	handler.ServeHTTP(headerlessRec, headerless)
	if got, want := headerlessRec.Code, http.StatusOK; got != want {
		t.Fatalf("headerless status = %d, want %d; body = %s", got, want, headerlessRec.Body.String())
	}

	matching := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	matching.Header.Set("Authorization", "Bearer "+bootstrapToken)
	matchingRec := httptest.NewRecorder()
	handler.ServeHTTP(matchingRec, matching)
	if got, want := matchingRec.Code, http.StatusOK; got != want {
		t.Fatalf("seeded bootstrap identity token status = %d, want %d; body = %s -- the seeded row was not resolved", got, want, matchingRec.Body.String())
	}

	nonMatching := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	nonMatching.Header.Set("Authorization", "Bearer wrong-token-not-seeded")
	nonMatchingRec := httptest.NewRecorder()
	handler.ServeHTTP(nonMatchingRec, nonMatching)
	if got, want := nonMatchingRec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("non-matching token status = %d, want %d; body = %s -- resolver must not admit arbitrary credentials", got, want, nonMatchingRec.Body.String())
	}
}

func TestAPIAuthPublicRouteOpenUnderEnforcement(t *testing.T) {
	t.Parallel()

	handler := buildAPIAuthHandler("", nil, &recordingScopedResolver{}, &fakeSessionResolver{})

	for _, path := range []string{"/api/v0/health", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("public %s status = %d, want %d", path, got, want)
		}
	}
}

func TestAPIAuthValidCookieServedUnderEnforcement(t *testing.T) {
	t.Parallel()

	// The cookie path self-enforces and runs BEFORE the dev-open gate, so a
	// valid session cookie is honored even when enforcement is on (OIDC configured).
	session := &fakeSessionResolver{}
	handler := buildAPIAuthHandler("", nil, &recordingScopedResolver{}, session)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.AddCookie(&http.Cookie{Name: query.BrowserSessionCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("cookie status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !session.called {
		t.Fatal("session resolver was not consulted for a cookie request")
	}
}

func TestAPIAuthCookielessDeniedUnderEnforcement(t *testing.T) {
	t.Parallel()

	session := &fakeSessionResolver{}
	handler := buildAPIAuthHandler("", nil, &recordingScopedResolver{}, session)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("cookieless status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if session.called {
		t.Fatal("session resolver consulted for a cookieless request")
	}
}
