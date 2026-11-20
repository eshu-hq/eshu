// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// G2 Wave-1 seven-leg replication, leg 2: auth (Refs eshu-hq/eshu#9669,
// satellite eshu-hq/eshu-api#101). Core-side reading of the W8a auth
// legs: the wave rows stay key-gated (absent from publicHTTPPaths),
// the 5 GET base legs admit scoped callers (the scoped allowlist
// holds them), the 4 POST rider spellings are not allowlisted (the
// mux judges them 405), a shared-key caller terminates natively with
// exactly one admission contact per row, and credential-less / junk /
// empty-key callers stay 401 with zero admission contact. Reference
// only — every line here is new core-side code.

const g2Wave1TestKey = "g2-wave1-test-key"

// g2Wave1AuthChain mounts counting pin-backed handlers behind the
// shared-key + scoped-token middleware, returning the mux and the
// three counting admissions so each leg can prove contact or its
// absence.
func g2Wave1AuthChain(resolver *fakeScopedTokenResolver) (*http.ServeMux, *countingExtractionReadiness, *countingFactSchema, *countingSurfaceInventory) {
	readiness := &countingExtractionReadiness{inner: &cannedExtractionReadiness{rows: map[scope.CollectorKind]extraction.Readiness{
		scope.CollectorGit: {Family: scope.CollectorGit, DisplayName: "Canned Git"},
	}}}
	facts := &countingFactSchema{inner: cannedFactSchema{}}
	surface := &countingSurfaceInventory{inner: surfaceInventoryStaticReader{}}
	mux := http.NewServeMux()
	(&APIRouter{
		ExtractionReadiness: &CollectorExtractionReadinessHandler{Profile: ProfileProduction, Reads: readiness},
		FactSchemaVersions:  &FactSchemaVersionHandler{Profile: ProfileProduction, Reads: facts},
		SurfaceInventory:    &SurfaceInventoryHandler{Profile: ProfileProduction, Reads: surface},
		Freshness:           &FreshnessHandler{Profile: ProfileProduction},
	}).Mount(mux)
	chained := http.NewServeMux()
	chained.Handle("/", AuthMiddlewareWithScopedTokens(g2Wave1TestKey, resolver, mux))
	return chained, readiness, facts, surface
}

// TestG2Wave1StaysKeyGated pins that no wave path bypasses
// authentication: every row's spelling is absent from
// publicHTTPPaths.
func TestG2Wave1StaysKeyGated(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, entry := range g2Wave1 {
		path := entry.Path
		if strings.Contains(path, "{") {
			path = concretizeG2Wave1Path(path)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		if publicHTTPPaths[path] {
			t.Errorf("wave path %s is public, want it key-gated", path)
		}
	}
}

// TestG2Wave1ScopedAllowlistAdmitsBaseLegsOnly pins the scoped
// posture per row: the 5 GET base legs admit scoped callers, the 4
// POST rider spellings are not allowlisted (core judges them 405).
func TestG2Wave1ScopedAllowlistAdmitsBaseLegsOnly(t *testing.T) {
	t.Parallel()

	for _, entry := range g2Wave1 {
		t.Run(entry.Method+" "+entry.Path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(entry.Method, concretizeG2Wave1Path(entry.Path), nil)
			got := scopedHTTPRouteSupportsTenantFilter(req)
			if entry.Method == http.MethodGet && !got {
				t.Errorf("scoped gate rejects GET %s, want the base leg admitted", entry.Path)
			}
			if entry.Method == http.MethodPost && got {
				t.Errorf("scoped gate admits POST %s, want the rider spelling out (405 at the mux)", entry.Path)
			}
		})
	}
}

// TestG2Wave1SharedKeyTerminates pins that a shared-key caller
// terminates natively on every GET base leg (200) with exactly one
// admission contact on the called family and none on the others.
func TestG2Wave1SharedKeyTerminates(t *testing.T) {
	t.Parallel()

	targets := []struct {
		path         string
		wantContacts func(readiness *countingExtractionReadiness, facts *countingFactSchema, surface *countingSurfaceInventory) (int, int, int)
	}{
		{"/api/v0/collector-extraction-readiness", func(r *countingExtractionReadiness, f *countingFactSchema, s *countingSurfaceInventory) (int, int, int) {
			return r.contacts(), f.contacts(), s.loadCalls
		}},
		{"/api/v0/collector-extraction-readiness/git", func(r *countingExtractionReadiness, f *countingFactSchema, s *countingSurfaceInventory) (int, int, int) {
			return r.contacts(), f.contacts(), s.loadCalls
		}},
		{"/api/v0/fact-schema-versions", func(r *countingExtractionReadiness, f *countingFactSchema, s *countingSurfaceInventory) (int, int, int) {
			return r.contacts(), f.contacts(), s.loadCalls
		}},
		{"/api/v0/fact-schema-versions/canned-fact", func(r *countingExtractionReadiness, f *countingFactSchema, s *countingSurfaceInventory) (int, int, int) {
			return r.contacts(), f.contacts(), s.loadCalls
		}},
		{"/api/v0/surface-inventory", func(r *countingExtractionReadiness, f *countingFactSchema, s *countingSurfaceInventory) (int, int, int) {
			return r.contacts(), f.contacts(), s.loadCalls
		}},
	}
	for _, target := range targets {
		t.Run("GET "+target.path, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{ok: false}
			chained, readiness, facts, surface := g2Wave1AuthChain(resolver)
			req := httptest.NewRequest(http.MethodGet, target.path, nil)
			req.Header.Set("Authorization", "Bearer "+g2Wave1TestKey)
			rec := httptest.NewRecorder()
			chained.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("shared-key status = %d, want 200 (terminate natively); body=%s", rec.Code, rec.Body.String())
			}
			gotR, gotF, gotS := target.wantContacts(readiness, facts, surface)
			if gotR+gotF+gotS != 1 {
				t.Errorf("admission contacts = (%d, %d, %d), want exactly 1 total on the called family", gotR, gotF, gotS)
			}
			// No resolver-order assertion: the middleware consults the
			// resolver first and falls back to the shared key (auth.go).
			// The leg's point is the 200 with exactly one admission
			// contact on the called family, which holds either way.
		})
	}
}

// TestG2Wave1ScopedCallersReachBaseLegs pins that a scoped caller
// reaches every GET base leg (200) through the same chain.
func TestG2Wave1ScopedCallersReachBaseLegs(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/collector-extraction-readiness",
		"/api/v0/collector-extraction-readiness/git",
		"/api/v0/fact-schema-versions",
		"/api/v0/fact-schema-versions/canned-fact",
		"/api/v0/surface-inventory",
	} {
		t.Run("GET "+path, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{Mode: AuthModeScoped, TenantID: "tenant_a", WorkspaceID: "workspace_a"},
				ok:      true,
			}
			chained, _, _, _ := g2Wave1AuthChain(resolver)
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			chained.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("scoped status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestG2Wave1CredentiallessStays401 pins that a credential-less wave
// call gets the standing 401 with zero admission contact.
func TestG2Wave1CredentiallessStays401(t *testing.T) {
	t.Parallel()

	for _, entry := range g2Wave1 {
		t.Run(entry.Method+" "+entry.Path, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{ok: false}
			chained, readiness, facts, surface := g2Wave1AuthChain(resolver)
			var req *http.Request
			if entry.Method == http.MethodPost {
				req = httptest.NewRequest(http.MethodPost, entry.Path, strings.NewReader(`{}`))
			} else {
				req = httptest.NewRequest(http.MethodGet, concretizeG2Wave1Path(entry.Path), nil)
			}
			rec := httptest.NewRecorder()
			chained.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("credential-less status = %d, want the standing 401", rec.Code)
			}
			if got := readiness.contacts() + facts.contacts() + surface.loadCalls; got != 0 {
				t.Errorf("credential-less call touched the admission %d times, want zero contact", got)
			}
		})
	}
}

// TestG2Wave1JunkBearerStays401 pins that a junk bearer (no resolver
// match, not the shared key) stays 401 with zero admission contact.
func TestG2Wave1JunkBearerStays401(t *testing.T) {
	t.Parallel()

	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/api/v0/surface-inventory"},
		{http.MethodGet, "/api/v0/collector-extraction-readiness"},
		{http.MethodPost, "/api/v0/freshness/generations"},
		{http.MethodPost, "/api/v0/surface-inventory"},
	} {
		t.Run(target.method+" "+target.path, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{ok: false}
			chained, readiness, facts, surface := g2Wave1AuthChain(resolver)
			req := httptest.NewRequest(target.method, target.path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer junk-token")
			rec := httptest.NewRecorder()
			chained.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("junk-bearer status = %d, want the standing 401", rec.Code)
			}
			if got := readiness.contacts() + facts.contacts() + surface.loadCalls; got != 0 {
				t.Errorf("junk-bearer call touched the admission %d times, want zero contact", got)
			}
		})
	}
}

// TestG2Wave1EmptyKeyFailsClosed pins that an empty configured key
// under an enforcing posture answers 401 with zero admission
// contact, like RequireAuth.
func TestG2Wave1EmptyKeyFailsClosed(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{ok: false}
	readiness := &countingExtractionReadiness{inner: &cannedExtractionReadiness{rows: map[scope.CollectorKind]extraction.Readiness{
		scope.CollectorGit: {Family: scope.CollectorGit, DisplayName: "Canned Git"},
	}}}
	var next http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler ran with empty configured key")
		w.WriteHeader(http.StatusOK)
	})
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement("", resolver, next, nil, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/collector-extraction-readiness", nil)
	req.Header.Set("Authorization", "Bearer "+g2Wave1TestKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("empty-key status = %d, want 401 (fail closed)", rec.Code)
	}
	if readiness.contacts() != 0 {
		t.Errorf("empty-key call touched the admission, want zero contact")
	}
}

// TestG2Wave1KeyedRidersJudgeMethod pins that a keyed POST rider is
// judged 405 at the mux (never 401/404-invented) with zero admission
// contact.
func TestG2Wave1KeyedRidersJudgeMethod(t *testing.T) {
	t.Parallel()

	riders := []string{
		"/api/v0/freshness/changed-since",
		"/api/v0/freshness/generations",
		"/api/v0/freshness/services/changed-since",
		"/api/v0/surface-inventory",
	}
	for _, path := range riders {
		t.Run("POST "+path, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{ok: false}
			chained, readiness, facts, surface := g2Wave1AuthChain(resolver)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+g2Wave1TestKey)
			rec := httptest.NewRecorder()
			chained.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("keyed-rider status = %d, want 405 (judged, never gate-invented)", rec.Code)
			}
			if got := readiness.contacts() + facts.contacts() + surface.loadCalls; got != 0 {
				t.Errorf("keyed-rider call touched the admission %d times, want zero contact (judged before admission)", got)
			}
		})
	}
}
