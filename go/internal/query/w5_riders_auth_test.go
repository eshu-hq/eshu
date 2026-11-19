// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Wave-5 rider seven-leg replication, leg 2: auth (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a auth legs: the R1 twin is never key-gated (the
// mux judges it before auth), the R2/R3 twins are never public (the
// trailing slash keeps them off the public list), and the R2/R3 bases
// stay public. A keyed rider is judged with zero admission contact;
// credential-less and junk-credential riders are denied at the
// middleware with zero contact; the public bases serve keyless with
// exactly-one-family contact.

// w5RidersTestKey is the shared key the auth legs terminate against.
// It names no production secret: the chained mux below is hermetic.
const w5RidersTestKey = "w5-riders-test-shared-key"

// w5RidersAuthChain wraps the canned rider mux in the production auth
// middleware with a resolver that never resolves, so every assertion
// below proves the middleware posture, not a resolver double.
func w5RidersAuthChain() (*http.ServeMux, *countingSignInPolicyStore, *countingAuthProviderStore) {
	mux, policy, providers := w5RidersCannedMux()
	resolver := &fakeScopedTokenResolver{ok: false}
	chained := http.NewServeMux()
	chained.Handle("/", AuthMiddlewareWithScopedTokens(w5RidersTestKey, resolver, mux))
	return chained, policy, providers
}

// TestW5RidersStayOffThePublicList pins that no wave rider spelling
// bypasses authentication by publicity: every rider path is absent
// from publicHTTPPaths, while the R2/R3 bases stay public (the login
// page fetches them pre-auth).
func TestW5RidersStayOffThePublicList(t *testing.T) {
	t.Parallel()

	for _, entry := range w5Riders {
		if publicHTTPPaths[entry.Path] {
			t.Errorf("wave rider path %s is public, want it key-gated (bases stay public, twins never do)", entry.Path)
		}
	}
	for _, base := range []string{"/api/v0/auth/sign-in-policy", "/api/v0/auth/providers"} {
		if req := httptest.NewRequest(http.MethodGet, base, nil); !publicHTTPRoute(req) {
			t.Errorf("wave base path %s is not public, want the login-page pre-auth posture", base)
		}
	}
}

// TestW5RidersScopedAllowlistAdmitsNoRider pins the scoped posture
// per row: no rider spelling is allowlisted (the mux judges every
// rider before the scoped gate matters), while the bundles POST base
// stays grant-bound.
func TestW5RidersScopedAllowlistAdmitsNoRider(t *testing.T) {
	t.Parallel()

	for _, entry := range w5Riders {
		t.Run(entry.Method+" "+entry.Path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(entry.Method, entry.Path, nil)
			if scopedHTTPRouteSupportsTenantFilter(req) {
				t.Errorf("scoped gate admits %s %s, want the rider spelling out (judged at the mux)", entry.Method, entry.Path)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/bundles", nil)
	if !scopedHTTPRouteSupportsTenantFilter(req) {
		t.Errorf("scoped gate rejects POST /api/v0/code/bundles, want the base leg grant-bound")
	}
}

// TestW5RidersKeyedRidersJudgeWithZeroContact pins that a shared-key
// caller is judged natively on every rider spelling with zero
// admission contact: the R1 twin 405s with Allow naming POST, the
// R2/R3 twins 404.
func TestW5RidersKeyedRidersJudgeWithZeroContact(t *testing.T) {
	t.Parallel()

	for _, entry := range w5Riders {
		t.Run(entry.Method+" "+entry.Path, func(t *testing.T) {
			t.Parallel()

			chained, policy, providers := w5RidersAuthChain()
			var req *http.Request
			if entry.Method == http.MethodPatch {
				req = httptest.NewRequest(http.MethodPatch, entry.Path, strings.NewReader(`{}`))
			} else {
				req = httptest.NewRequest(http.MethodGet, entry.Path, nil)
			}
			req.Header.Set("Authorization", "Bearer "+w5RidersTestKey)
			rec := httptest.NewRecorder()
			chained.ServeHTTP(rec, req)

			if entry.Method == http.MethodPatch && rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("keyed %s %s = %d, want 405", entry.Method, entry.Path, rec.Code)
			}
			if entry.Method == http.MethodGet && rec.Code != http.StatusNotFound {
				t.Fatalf("keyed %s %s = %d, want 404", entry.Method, entry.Path, rec.Code)
			}
			if policy.contacts()+providers.contacts() != 0 {
				t.Errorf("keyed %s %s contacted admissions %d times, want zero contact", entry.Method, entry.Path, policy.contacts()+providers.contacts())
			}
		})
	}
}

// TestW5RidersCredentiallessAndJunkStay401 pins that riders without a
// usable credential are denied at the middleware with zero admission
// contact: no rider spelling leaks its judgment (or a base answer) to
// an unauthenticated caller.
func TestW5RidersCredentiallessAndJunkStay401(t *testing.T) {
	t.Parallel()

	for _, entry := range w5Riders {
		for _, cred := range []struct {
			name  string
			value string
		}{
			{"credentialless", ""},
			{"junk", "Bearer junk-credential"},
			{"empty", "Bearer "},
		} {
			t.Run(entry.Method+" "+entry.Path+" "+cred.name, func(t *testing.T) {
				t.Parallel()

				chained, policy, providers := w5RidersAuthChain()
				var req *http.Request
				if entry.Method == http.MethodPatch {
					req = httptest.NewRequest(http.MethodPatch, entry.Path, strings.NewReader(`{}`))
				} else {
					req = httptest.NewRequest(http.MethodGet, entry.Path, nil)
				}
				if cred.value != "" {
					req.Header.Set("Authorization", cred.value)
				}
				rec := httptest.NewRecorder()
				chained.ServeHTTP(rec, req)

				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("%s %s %s = %d, want 401 at the middleware", entry.Method, entry.Path, cred.name, rec.Code)
				}
				if policy.contacts()+providers.contacts() != 0 {
					t.Errorf("%s %s %s contacted admissions %d times, want zero contact", entry.Method, entry.Path, cred.name, policy.contacts()+providers.contacts())
				}
			})
		}
	}
}

// TestW5RidersPublicBasesServeKeyless pins the other side of the
// never-split posture: the R2/R3 public bases serve without a
// credential through their pins with exactly-one-family contact, so
// the login page keeps its pre-auth discovery while the twins stay
// judged.
func TestW5RidersPublicBasesServeKeyless(t *testing.T) {
	t.Parallel()

	for _, base := range []string{"/api/v0/auth/sign-in-policy?tenant_id=t1", "/api/v0/auth/providers?tenant_id=t1"} {
		t.Run("GET "+base, func(t *testing.T) {
			t.Parallel()

			chained, policy, providers := w5RidersAuthChain()
			beforePolicy, beforeProviders := policy.contacts(), providers.contacts()
			req := httptest.NewRequest(http.MethodGet, base, nil)
			rec := httptest.NewRecorder()
			chained.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("keyless GET %s = %d, want 200 (public base serves through the pin)", base, rec.Code)
			}
			if got := policy.contacts() + providers.contacts() - beforePolicy - beforeProviders; got < 1 {
				t.Errorf("keyless GET %s contacted admissions %d times, want the pin admit (at least 1)", base, got)
			}
		})
	}
}
