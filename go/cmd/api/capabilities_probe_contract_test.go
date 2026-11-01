// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/capability"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scopedtoken"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// This suite is the permanent contract behind the eshu-mcp satellite's
// credential-admission probe (eshu-hq/eshu-mcp#50). The satellite forwards a
// caller's credential to core as `GET /api/v0/capabilities` and treats 200 as
// "core admits this credential" and 401 as "core refuses it". It uses this
// route and not GET /api/v0/auth/profile because profile answers 401 for any
// credential that carries no subject hash (profile_handler.go: the
// SubjectIDHash == "" guard), which includes a perfectly valid registry token
// with no subject_id_hash. If this suite goes red, the satellite's admission
// check breaks for real credentials; coordinate with eshu-mcp before changing
// an expectation here.
//
// It drives the production auth glue (wrapAPIAuth, scopedtoken.ChainResolvers
// over the real Postgres identity resolver backed by a query stub, plus a real
// file-registry resolver) and the real capability.Handler mounted through
// query.APIRouter. Out of scope, deliberately:
//   - OIDC bearer tokens: the chain is built with a nil OIDC resolver, so this
//     test says nothing about JWT admission or refusal.
//   - Browser-session cookies: the satellite forwards bearer credentials.
//   - The identity SQL itself: the stub restates the revoked/expires predicate
//     (see probeIdentityToken); only a live Postgres proves the SQL text.
//   - An identity-store outage answers a retryable 503, not the 200/401 the
//     satellite probe contract is about; one case below pins it so a change to
//     it is a conscious one.

const (
	probeSharedKey   = "probe-shared-api-key"
	capabilitiesPath = "/api/v0/capabilities"
)

// probeRegistryEntry builds one scoped-token registry entry for the file
// resolver.
type probeRegistryEntry struct {
	token         string
	subjectClass  string
	subjectIDHash string
	allScopes     bool
	repoIDs       []string
}

func (e probeRegistryEntry) document() map[string]any {
	sum := sha256.Sum256([]byte(e.token))
	doc := map[string]any{
		"token_sha256": hex.EncodeToString(sum[:]),
		"tenant_id":    "tenant_a",
		"workspace_id": "workspace_a",
		"all_scopes":   e.allScopes,
	}
	if e.subjectClass != "" {
		doc["subject_class"] = e.subjectClass
	}
	if e.subjectIDHash != "" {
		doc["subject_id_hash"] = e.subjectIDHash
	}
	if len(e.repoIDs) > 0 {
		doc["allowed_repository_ids"] = e.repoIDs
	}
	return doc
}

func probeRegistryResolver(t *testing.T, entries ...probeRegistryEntry) query.ScopedTokenResolver {
	t.Helper()
	tokens := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		tokens = append(tokens, entry.document())
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "tokens": tokens})
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

// probeCapabilitiesHandler assembles the production auth glue in front of the
// real capabilities route under the given ESHU_GOVERNANCE_MODE value.
func probeCapabilitiesHandler(
	t *testing.T,
	governanceMode string,
	registry query.ScopedTokenResolver,
	identity probeIdentityQueryer,
) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	(&query.APIRouter{
		Capabilities: &capability.Handler{Profile: querycontract.ProfileProduction},
	}).Mount(mux)

	identityResolver := scopedtoken.NewPostgresIdentityResolver(pgstatus.NewScopedAPITokenStore(identity))
	chain := scopedtoken.ChainResolvers(identityResolver, nil, registry)
	return wrapAPIAuth(
		probeSharedKey,
		chain,
		&fakeSessionResolver{},
		mux,
		nil,
		query.GovernanceStatusConfig{Mode: governanceMode},
		authEnforcementConfigured(probeSharedKey, registry, nil),
	)
}

func probeGet(handler http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, capabilitiesPath, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestCapabilitiesProbeAdmitsEveryValidCredentialClass pins that every valid
// credential class reaches the real catalog handler and gets 200 with a
// non-empty catalog, under every governance mode, including
// hosted_multi_tenant where an all-scope bearer is refused on grant-bound
// routes.
func TestCapabilitiesProbeAdmitsEveryValidCredentialClass(t *testing.T) {
	t.Parallel()

	const (
		registryWithSubject    = "registry-token-with-subject"
		registryNoSubject      = "registry-token-no-subject"
		registryAllScopesNoSub = "registry-token-all-scopes-no-subject"
		registryMinimal        = "registry-token-minimal-scope"
		personalToken          = "personal-identity-token"
		servicePrincipalToken  = "service-principal-identity-token"
		personalNoFeatures     = "personal-token-no-features"
		personalUnrelatedGrant = "personal-token-unrelated-feature"
		personalNonExpiring    = "personal-token-null-expires-at"
		personalFutureExpiry   = "personal-token-future-expires-at"
	)
	future := time.Now().Add(24 * time.Hour)
	registry := probeRegistryResolver(t,
		probeRegistryEntry{
			token: registryWithSubject, subjectClass: "service", allScopes: true,
			subjectIDHash: "sha256:0123456789abcdef0123456789abcdef",
		},
		// Registry tokens with NO subject: /api/v0/auth/profile answers 401 for
		// these, which is why the satellite probes capabilities instead.
		probeRegistryEntry{token: registryNoSubject, repoIDs: []string{"repo-1"}},
		probeRegistryEntry{token: registryAllScopesNoSub, allScopes: true},
		// Minimal scope: no all_scopes, no repository grant, no subject.
		probeRegistryEntry{token: registryMinimal},
	)
	identity := newProbeIdentityQueryer(
		probeIdentityToken{
			tokenHash: pgstatus.ScopedAPITokenHash(personalToken), tokenClass: "personal",
			tenantID: "tenant_a", workspaceID: "workspace_a",
			subjectIDHash: "sha256:aaaaaaaaaaaaaaaa", policyRevisionHash: "sha256:bbbbbbbbbbbbbbbb",
			roleID: "role-reader", features: []string{"search"},
		},
		probeIdentityToken{
			tokenHash: pgstatus.ScopedAPITokenHash(servicePrincipalToken), tokenClass: "service_principal",
			tenantID: "tenant_a", workspaceID: "workspace_a",
			servicePrincipalID: "sp-1", policyRevisionHash: "sha256:bbbbbbbbbbbbbbbb",
			roleID: "role-sp", features: []string{"search"},
		},
		// Permission catalog enforced with an empty feature allow-list.
		probeIdentityToken{
			tokenHash: pgstatus.ScopedAPITokenHash(personalNoFeatures), tokenClass: "personal",
			tenantID: "tenant_a", workspaceID: "workspace_a",
			subjectIDHash: "sha256:cccccccccccccccc", policyRevisionHash: "sha256:bbbbbbbbbbbbbbbb",
			roleID: "role-empty",
		},
		// Permission catalog enforced with one feature unrelated to capabilities.
		probeIdentityToken{
			tokenHash: pgstatus.ScopedAPITokenHash(personalUnrelatedGrant), tokenClass: "personal",
			tenantID: "tenant_a", workspaceID: "workspace_a",
			subjectIDHash: "sha256:dddddddddddddddd", policyRevisionHash: "sha256:bbbbbbbbbbbbbbbb",
			roleID: "role-unrelated", features: []string{"vulnerability-suppression-write"},
		},
		// Non-expiring: expires_at IS NULL.
		probeIdentityToken{
			tokenHash: pgstatus.ScopedAPITokenHash(personalNonExpiring), tokenClass: "personal",
			tenantID: "tenant_a", workspaceID: "workspace_a",
			subjectIDHash: "sha256:eeeeeeeeeeeeeeee", policyRevisionHash: "sha256:bbbbbbbbbbbbbbbb",
			roleID: "role-reader",
		},
		// Not yet expired: expires_at in the future.
		probeIdentityToken{
			tokenHash: pgstatus.ScopedAPITokenHash(personalFutureExpiry), tokenClass: "personal",
			tenantID: "tenant_a", workspaceID: "workspace_a",
			subjectIDHash: "sha256:ffffffffffffffff", policyRevisionHash: "sha256:bbbbbbbbbbbbbbbb",
			roleID: "role-reader", expiresAt: &future,
		},
	)

	credentials := []struct {
		name  string
		token string
	}{
		{"shared API key", probeSharedKey},
		{"registry token with subject", registryWithSubject},
		{"registry token without subject, repo grant", registryNoSubject},
		{"registry all-scope token without subject", registryAllScopesNoSub},
		{"registry minimally scoped token", registryMinimal},
		{"personal identity token", personalToken},
		{"service-principal identity token", servicePrincipalToken},
		{"personal token, permission catalog enforced, no features", personalNoFeatures},
		{"personal token, only an unrelated feature granted", personalUnrelatedGrant},
		{"personal token, non-expiring (expires_at NULL)", personalNonExpiring},
		{"personal token, expires_at in the future", personalFutureExpiry},
	}

	for _, mode := range []string{"", "hosted_single_tenant", "hosted_multi_tenant"} {
		handler := probeCapabilitiesHandler(t, mode, registry, identity)
		for _, tc := range credentials {
			t.Run("mode="+mode+"/"+tc.name, func(t *testing.T) {
				rec := probeGet(handler, "Bearer "+tc.token)
				if got, want := rec.Code, http.StatusOK; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
				}
				var body struct {
					Data struct {
						Capabilities []json.RawMessage `json:"capabilities"`
					} `json:"data"`
					Capabilities []json.RawMessage `json:"capabilities"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
				}
				if len(body.Capabilities) == 0 && len(body.Data.Capabilities) == 0 {
					t.Fatalf("admitted response carried no capabilities; body = %s", rec.Body.String())
				}
			})
		}
	}
}

// TestCapabilitiesProbeRefusesInvalidCredentials pins that an invalid, unknown,
// revoked, expired, or absent credential is refused with 401 and the
// WWW-Authenticate challenge the satellite relies on, and never reaches the
// catalog handler.
func TestCapabilitiesProbeRefusesInvalidCredentials(t *testing.T) {
	t.Parallel()

	const validRegistry = "registry-token-valid"
	registry := probeRegistryResolver(t, probeRegistryEntry{token: validRegistry, allScopes: true})
	// Revoked and expired rows are seeded for real. The stub applies the
	// revoked_at / expires_at predicate against the now the resolver passes, so
	// these 401s depend on the resolver's clock and on the predicate, unlike an
	// unknown hash, which the garbage-bearer case covers separately.
	past := time.Now().Add(-time.Hour)
	seed := func(token string) probeIdentityToken {
		return probeIdentityToken{
			tokenHash: pgstatus.ScopedAPITokenHash(token), tokenClass: "personal",
			tenantID: "tenant_a", workspaceID: "workspace_a",
			subjectIDHash: "sha256:aaaaaaaaaaaaaaaa", policyRevisionHash: "sha256:bbbbbbbbbbbbbbbb",
			roleID: "role-reader", features: []string{"search"},
		}
	}
	revoked, expired := seed("revoked-identity-token"), seed("expired-identity-token")
	revoked.revoked = true
	expired.expiresAt = &past
	identity := newProbeIdentityQueryer(revoked, expired)

	cases := []struct {
		name          string
		authorization string
	}{
		{"absent credential", ""},
		{"garbage bearer", "Bearer not-a-real-credential"},
		{"revoked identity token", "Bearer revoked-identity-token"},
		{"expired identity token", "Bearer expired-identity-token"},
		{"empty bearer", "Bearer "},
		{"non-bearer scheme", "Basic " + probeSharedKey},
		{"shared key without scheme", probeSharedKey},
		{"near-miss of shared key", "Bearer " + probeSharedKey + "x"},
		{"near-miss of registry token", "Bearer " + validRegistry + "x"},
	}
	for _, mode := range []string{"", "hosted_multi_tenant"} {
		handler := probeCapabilitiesHandler(t, mode, registry, identity)
		for _, tc := range cases {
			t.Run("mode="+mode+"/"+tc.name, func(t *testing.T) {
				rec := probeGet(handler, tc.authorization)
				if got, want := rec.Code, http.StatusUnauthorized; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
				}
				if challenge := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(challenge, "Bearer") {
					t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", challenge)
				}
				if strings.Contains(rec.Body.String(), `"capabilities"`) {
					t.Fatalf("refused response leaked catalog body: %s", rec.Body.String())
				}
			})
		}
	}
}

// TestCapabilitiesProbeRouteStaysTenantDataFree pins the route-class
// decision the probe depends on: the capabilities route is classified as
// needing no caller grant, so an all-scope bearer is admitted even where the
// governance mode refuses it on grant-bound routes, while that same bearer is
// still refused (403) on a grant-bound route. If the route class changes, the
// first assertion fails; if the policy stops refusing, the control fails and
// this test no longer proves anything.
func TestCapabilitiesProbeRouteStaysTenantDataFree(t *testing.T) {
	t.Parallel()

	const allScopeToken = "registry-all-scope-token"
	registry := probeRegistryResolver(t, probeRegistryEntry{token: allScopeToken, allScopes: true})
	handler := probeCapabilitiesHandler(t, "hosted_multi_tenant", registry, newProbeIdentityQueryer())

	if rec := probeGet(handler, "Bearer "+allScopeToken); rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, want 200 for an all-scope bearer under hosted_multi_tenant; body = %s",
			capabilitiesPath, rec.Code, rec.Body.String())
	}

	// Control: a grant-bound route refuses the same bearer under the same mode.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer "+allScopeToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("control /api/v0/repositories status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

// TestCapabilitiesProbeIdentityStoreOutageAnswers503 pins that an unreachable
// identity store is a retryable 503 with no challenge, not a 401: the
// credential was never judged. This sits outside the 200/401 admission
// contract, but the satellite must be able to tell "refused" from "could not
// check", so a change here should be deliberate. The identity resolver runs
// first in the chain, so during an outage every credential class, including
// the shared key and registry tokens, gets this 503; a caller must treat 503 as
// "unknown" for all of them, never as a refusal.
func TestCapabilitiesProbeIdentityStoreOutageAnswers503(t *testing.T) {
	t.Parallel()

	identity := probeIdentityQueryer{err: driver.ErrBadConn}
	handler := probeCapabilitiesHandler(t, "", probeRegistryResolver(t), identity)

	rec := probeGet(handler, "Bearer any-identity-token")
	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if challenge := rec.Header().Get("WWW-Authenticate"); challenge != "" {
		t.Fatalf("WWW-Authenticate = %q, want none on an outage 503", challenge)
	}
}
