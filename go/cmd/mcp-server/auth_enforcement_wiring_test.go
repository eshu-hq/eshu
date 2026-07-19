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

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/scopedtoken"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// This suite builds the mcp-server auth middleware EXACTLY as wireAPI does —
// the real always-wired Postgres identity resolver, a real
// scopedtoken.ChainResolvers(identity, oidc, file) chain, the real
// authEnforcementConfigured predicate, and the real production constructor
// AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement — rather than
// hand-built isolated resolvers. That real composition is what the first fix
// attempt (gating on resolver != nil) got wrong: the identity resolver is
// always non-nil, so a resolver-presence gate would 401 the demo. mcp-server
// mounts no browser-session resolver.

// stubExecQueryer is an empty identity-token store standing in for the Postgres
// connection behind the always-wired identity resolver. It returns no rows so
// the identity resolver reports "not found" (no error) for any presented
// credential, letting the ChainResolvers chain fall through to the OIDC/file
// resolver or the shared-key comparison — exactly as an unseeded token store
// would. Headerless requests never present a credential, so it is not queried
// on those paths at all.
type stubExecQueryer struct{}

func (stubExecQueryer) QueryContext(context.Context, string, ...any) (pgstatus.Rows, error) {
	return emptyRows{}, nil
}

func (stubExecQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("stubExecQueryer: unexpected exec")
}

// emptyRows is a zero-row result set.
type emptyRows struct{}

func (emptyRows) Next() bool        { return false }
func (emptyRows) Scan(...any) error { return errors.New("emptyRows: no rows") }
func (emptyRows) Err() error        { return nil }
func (emptyRows) Close() error      { return nil }

// recordingScopedResolver represents a configured-but-not-matching scoped
// resolver (e.g. the OIDC bearer resolver when ESHU_AUTH_RESOURCE_URI is set).
// It records whether it was consulted so tests can assert headerless requests
// are denied before any resolution.
type recordingScopedResolver struct{ called bool }

func (r *recordingScopedResolver) ResolveScopedToken(context.Context, string) (query.AuthContext, bool, error) {
	r.called = true
	return query.AuthContext{}, false, nil
}

// buildMCPAuthHandler assembles the middleware the way cmd/mcp-server/wiring.go
// does, from the same inputs.
func buildMCPAuthHandler(apiKey string, fileResolver, oidcResolver query.ScopedTokenResolver) http.Handler {
	identityResolver := scopedtoken.NewPostgresIdentityResolver(
		pgstatus.NewScopedAPITokenStore(stubExecQueryer{}),
	)
	enforcement := authEnforcementConfigured(apiKey, fileResolver, oidcResolver)
	scopedTokenResolver := scopedtoken.ChainResolvers(identityResolver, oidcResolver, fileResolver)
	return query.AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		apiKey, scopedTokenResolver, okHandler(), nil, enforcement,
	)
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

// writeScopedTokenFile writes a minimal scoped-token registry file granting the
// given token all-scopes read for one tenant/workspace, and returns a real
// file-registry resolver over it.
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

func TestMCPAuthDemoConfigServesHeaderlessOpen(t *testing.T) {
	t.Parallel()

	// Demo: no shared key, no scoped-token file, no OIDC audience. The identity
	// resolver is still present (always). Headerless reads stay open.
	handler := buildMCPAuthHandler("", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("headerless status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestMCPAuthOIDCConfiguredDeniesHeaderless(t *testing.T) {
	t.Parallel()

	// A non-nil OIDC bearer resolver (ESHU_AUTH_RESOURCE_URI set), no shared
	// key. This is THE real-bug regression: pre-fix, the empty shared key
	// short-circuited to dev-open before the resolver chain was consulted.
	oidc := &recordingScopedResolver{}
	handler := buildMCPAuthHandler("", nil, oidc)

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

func TestMCPAuthTokenFileConfiguredDeniesHeaderlessAdmitsToken(t *testing.T) {
	t.Parallel()

	const token = "file-registry-token-secret"
	fileResolver := writeScopedTokenFile(t, token)
	handler := buildMCPAuthHandler("", fileResolver, nil)

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

func TestMCPAuthSharedKeyDeniesHeaderlessAdmitsKey(t *testing.T) {
	t.Parallel()

	const key = "shared-api-key"
	handler := buildMCPAuthHandler(key, nil, nil)

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

// TestMCPAuthBootstrapIdentityOnlyServesHeaderlessOpen proves the
// bootstrap-identity-only case (#4962/#4963 seeded identities, NONE of the
// three explicit credential knobs) genuinely, not by coincidence with the
// no-identity demo case. TestMCPAuthDemoConfigServesHeaderlessOpen's stub
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
func TestMCPAuthBootstrapIdentityOnlyServesHeaderlessOpen(t *testing.T) {
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
	handler := query.AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		"", scopedTokenResolver, okHandler(), nil, enforcement,
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

func TestMCPAuthPublicRouteOpenUnderEnforcement(t *testing.T) {
	t.Parallel()

	// Enforcement on (OIDC configured); public routes are still served headerless.
	handler := buildMCPAuthHandler("", nil, &recordingScopedResolver{})

	for _, path := range []string{"/api/v0/health", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("public %s status = %d, want %d", path, got, want)
		}
	}
}
