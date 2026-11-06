// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config_test

// D0 termination-transport shim: byte-diff harness, fail-closed proof, and latency benchmarks. See termination_transport_shim_test.go for the theory.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin/provider/config"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

var (
	shimFixtureCreated = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	shimFixtureUpdated = time.Date(2026, 3, 5, 6, 7, 8, 0, time.UTC)
	shimFixtureActive  = time.Date(2026, 3, 6, 7, 8, 9, 0, time.UTC)
)

func shimFixtureStore() *shimFakeStore {
	pc1 := config.Detail{
		ProviderConfigID:      "pc_1",
		ProviderKind:          "oidc",
		Status:                "active",
		ActiveRevisionID:      "rev_1",
		Configuration:         map[string]any{"issuer": "https://idp.example.com"},
		HasSecret:             true,
		SecretFingerprint:     "abc12345",
		SecretKeyID:           "k1",
		ShadowedByEnvironment: true,
		ManagedBy:             "environment",
		CreatedAt:             shimFixtureCreated,
		UpdatedAt:             shimFixtureUpdated,
	}
	pc2 := config.Detail{
		ProviderConfigID: "pc_2",
		ProviderKind:     "github",
		Status:           "draft",
		ManagedBy:        "database",
		CreatedAt:        shimFixtureCreated,
		UpdatedAt:        shimFixtureUpdated,
	}
	pc9 := config.Detail{
		ProviderConfigID: "pc_9",
		ProviderKind:     "saml",
		Status:           "active",
		ManagedBy:        "database",
		CreatedAt:        shimFixtureCreated,
		UpdatedAt:        shimFixtureUpdated,
	}
	rev1 := config.RevisionItem{
		RevisionID:  "rev_1",
		Status:      "active",
		HasSecret:   true,
		CreatedAt:   shimFixtureCreated,
		ActivatedAt: shimFixtureActive,
	}
	rev0 := config.RevisionItem{
		RevisionID:   "rev_0",
		Status:       "superseded",
		CreatedAt:    shimFixtureCreated,
		ActivatedAt:  shimFixtureCreated,
		SupersededAt: shimFixtureActive,
	}
	return &shimFakeStore{
		details: map[string]map[string]config.Detail{
			"tenant_a": {"pc_1": pc1, "pc_2": pc2},
			"tenant_b": {"pc_9": pc9},
		},
		lists: map[string][]config.Detail{
			"tenant_a": {pc1, pc2},
			"tenant_b": {pc9},
		},
		revs: map[string]map[string][]config.RevisionItem{
			"tenant_a": {"pc_1": {rev1, rev0}, "pc_2": {}},
		},
	}
}

// shimAdminAuth mirrors the in-package providerConfigAdminAuth fixture:
// shared-mode, all-scopes, tenant-bound (unenforced catalog).
func shimAdminAuth(tenant string) auth.AuthContext {
	return auth.AuthContext{Mode: auth.AuthModeShared, TenantID: tenant, AllScopes: true}
}

func shimEnforcedAuth(tenant string, features ...string) auth.AuthContext {
	return auth.AuthContext{
		Mode:                      auth.AuthModeScoped,
		TenantID:                  tenant,
		AllScopes:                 true,
		PermissionCatalogEnforced: true,
		AllowedPermissionFeatures: features,
	}
}

// shimWorld wires the reference handler and the shim over the same
// fixture store. One world per case: the store counters prove denied
// requests never reach the port.
type shimWorld struct {
	store     *shimFakeStore
	reference http.Handler
	shim      http.Handler
}

func newShimWorld(t *testing.T, store *shimFakeStore) *shimWorld {
	t.Helper()
	refMux := http.NewServeMux()
	(&config.ReadHandler{Store: store}).Mount(refMux)
	portSrv := httptest.NewServer((&shimPortServer{store: store}).handler())
	t.Cleanup(portSrv.Close)
	destMux := http.NewServeMux()
	(&shimDestReadHandler{PortBaseURL: portSrv.URL, Client: portSrv.Client()}).Mount(destMux)
	return &shimWorld{store: store, reference: refMux, shim: destMux}
}

func shimServe(t *testing.T, h http.Handler, method, target string, authCtx *auth.AuthContext) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if authCtx != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), *authCtx))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body), res.Header
}

type shimCase struct {
	name   string
	method string
	target string
	auth   *auth.AuthContext
}

func shimAuthPtr(a auth.AuthContext) *auth.AuthContext { return &a }

// TestShimByteParityWithRelay is the D0 byte-diff harness: every case
// must produce identical status, Content-Type, and body bytes on the
// terminated shim and the in-tree reference handler.
func TestShimByteParityWithRelay(t *testing.T) {
	t.Parallel()

	adminA := shimAuthPtr(shimAdminAuth("tenant_a"))
	adminB := shimAuthPtr(shimAdminAuth("tenant_b"))
	nonScopes := shimAuthPtr(auth.AuthContext{Mode: auth.AuthModeShared, TenantID: "tenant_a"})
	noTenant := shimAuthPtr(auth.AuthContext{Mode: auth.AuthModeShared, AllScopes: true})
	enforcedDeny := shimAuthPtr(shimEnforcedAuth("tenant_a", "other_feature"))
	enforcedAllow := shimAuthPtr(shimEnforcedAuth("tenant_a", shimPermissionFeature))

	cases := []shimCase{
		{"list ok", http.MethodGet, "/api/v0/auth/admin/provider-configs", adminA},
		{"list tenant isolation", http.MethodGet, "/api/v0/auth/admin/provider-configs", adminB},
		{"list non-all-scopes", http.MethodGet, "/api/v0/auth/admin/provider-configs", nonScopes},
		{"list empty tenant", http.MethodGet, "/api/v0/auth/admin/provider-configs", noTenant},
		{"list no auth", http.MethodGet, "/api/v0/auth/admin/provider-configs", nil},
		{"list feature denied", http.MethodGet, "/api/v0/auth/admin/provider-configs", enforcedDeny},
		{"list feature allowed", http.MethodGet, "/api/v0/auth/admin/provider-configs", enforcedAllow},
		{"get found", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1", adminA},
		{"get minimal row", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_2", adminA},
		{"get cross-tenant miss", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1", adminB},
		{"get unknown", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_nope", adminA},
		{"get denied", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1", enforcedDeny},
		{"revisions found", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1/revisions", adminA},
		{"revisions empty", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_2/revisions", adminA},
		{"revisions unknown", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_nope/revisions", adminA},
		{"revisions denied", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1/revisions", nonScopes},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newShimWorld(t, shimFixtureStore())

			refStatus, refBody, refHeader := shimServe(t, w.reference, tc.method, tc.target, tc.auth)
			shimStatus, shimBody, shimHeader := shimServe(t, w.shim, tc.method, tc.target, tc.auth)

			if shimStatus != refStatus {
				t.Errorf("status = %d, want reference %d (body %q)", shimStatus, refStatus, shimBody)
			}
			if got := shimHeader.Get("Content-Type"); got != refHeader.Get("Content-Type") {
				t.Errorf("Content-Type = %q, want reference %q", got, refHeader.Get("Content-Type"))
			}
			if shimBody != refBody {
				t.Errorf("body differs:\nshim: %s\nref:  %s", shimBody, refBody)
			}
		})
	}
}

// TestShimByteParityStoreFailure pins the 500 envelope path: a failing
// port answers the fixed client message (never the store error) on both
// sides, byte-identical.
func TestShimByteParityStoreFailure(t *testing.T) {
	t.Parallel()

	adminA := shimAuthPtr(shimAdminAuth("tenant_a"))
	targets := []string{
		"/api/v0/auth/admin/provider-configs",
		"/api/v0/auth/admin/provider-configs/pc_1",
		"/api/v0/auth/admin/provider-configs/pc_1/revisions",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			store := shimFixtureStore()
			store.forceErr = io.EOF
			w := newShimWorld(t, store)

			refStatus, refBody, _ := shimServe(t, w.reference, http.MethodGet, target, adminA)
			shimStatus, shimBody, _ := shimServe(t, w.shim, http.MethodGet, target, adminA)

			if refStatus != http.StatusInternalServerError {
				t.Fatalf("reference status = %d, want 500", refStatus)
			}
			if shimStatus != refStatus || shimBody != refBody {
				t.Errorf("shim (%d %q) != reference (%d %q)", shimStatus, shimBody, refStatus, refBody)
			}
			if strings.Contains(shimBody, "EOF") {
				t.Errorf("store error leaked into the body: %q", shimBody)
			}
		})
	}
}

// TestShimByteParityStoreUnavailable pins the nil-store 503 on both sides
// plus deny precedence: an unauthenticated call against a nil store still
// answers 503 (readiness wins), identically.
func TestShimByteParityStoreUnavailable(t *testing.T) {
	t.Parallel()

	adminA := shimAuthPtr(shimAdminAuth("tenant_a"))
	newNilWorld := func() (http.Handler, http.Handler) {
		refMux := http.NewServeMux()
		(&config.ReadHandler{Store: nil}).Mount(refMux)
		destMux := http.NewServeMux()
		(&shimDestReadHandler{PortBaseURL: "", Client: nil}).Mount(destMux)
		return refMux, destMux
	}

	for name, authCtx := range map[string]*auth.AuthContext{"authed": adminA, "unauthed": nil} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ref, shim := newNilWorld()
			refStatus, refBody, _ := shimServe(t, ref, http.MethodGet, "/api/v0/auth/admin/provider-configs", authCtx)
			shimStatus, shimBody, _ := shimServe(t, shim, http.MethodGet, "/api/v0/auth/admin/provider-configs", authCtx)
			if refStatus != http.StatusServiceUnavailable {
				t.Fatalf("reference status = %d, want 503", refStatus)
			}
			if shimStatus != refStatus || shimBody != refBody {
				t.Errorf("shim (%d %q) != reference (%d %q)", shimStatus, shimBody, refStatus, refBody)
			}
		})
	}
}

// TestShimIdempotentRepeat proves GET repeatability on the terminated
// path: two identical calls return identical bytes (idempotency of the
// read family, no per-call nonce or timestamp drift).
func TestShimIdempotentRepeat(t *testing.T) {
	t.Parallel()

	w := newShimWorld(t, shimFixtureStore())
	adminA := shimAuthPtr(shimAdminAuth("tenant_a"))
	for _, target := range []string{
		"/api/v0/auth/admin/provider-configs",
		"/api/v0/auth/admin/provider-configs/pc_1",
		"/api/v0/auth/admin/provider-configs/pc_1/revisions",
	} {
		firstStatus, firstBody, _ := shimServe(t, w.shim, http.MethodGet, target, adminA)
		secondStatus, secondBody, _ := shimServe(t, w.shim, http.MethodGet, target, adminA)
		if firstStatus != secondStatus || firstBody != secondBody {
			t.Errorf("%s: repeat differs: (%d %q) vs (%d %q)", target, firstStatus, firstBody, secondStatus, secondBody)
		}
	}
}

// TestShimPortFailsClosedWithoutBounds proves authz is atomic core-side:
// direct port calls without valid asserted bounds fail closed AND record
// zero store calls — the destination holds no handle that bypasses the
// gate, because it holds no handle at all.
func TestShimPortFailsClosedWithoutBounds(t *testing.T) {
	t.Parallel()

	store := shimFixtureStore()
	srv := httptest.NewServer((&shimPortServer{store: store}).handler())
	t.Cleanup(srv.Close)

	totalCalls := func() int {
		total := 0
		for _, n := range store.calls {
			total += n
		}
		return total
	}
	attack := func(path string, headers map[string]string) (status, newCalls int) {
		before := totalCalls()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path+"?tenant_id=tenant_a", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("port call failed: %v", err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, totalCalls() - before
	}

	bearer := map[string]string{shimHeaderAllScopes: "true", shimHeaderTenant: "tenant_a"}
	for _, path := range []string{
		"/v0/port/provider-configs",
		"/v0/port/provider-configs/detail",
		"/v0/port/provider-configs/revisions",
	} {
		if status, calls := attack(path, nil); status != http.StatusForbidden || calls != 0 {
			t.Errorf("%s no-bounds: status = %d (want 403), new store calls = %d (want 0)", path, status, calls)
		}
		if status, calls := attack(path, map[string]string{shimHeaderAllScopes: "true"}); status != http.StatusForbidden || calls != 0 {
			t.Errorf("%s tenantless: status = %d (want 403), new store calls = %d (want 0)", path, status, calls)
		}
		if status, calls := attack(path, map[string]string{
			shimHeaderAllScopes: "true", shimHeaderTenant: "tenant_a",
			shimHeaderEnforced: "true", shimHeaderFeatures: "other_feature",
		}); status != http.StatusForbidden || calls != 0 {
			t.Errorf("%s feature-denied: status = %d (want 403), new store calls = %d (want 0)", path, status, calls)
		}
		if status, calls := attack(path, bearer); status != http.StatusOK && path == "/v0/port/provider-configs" {
			t.Errorf("%s valid bounds: status = %d (want 200)", path, status)
		} else if calls == 0 && path == "/v0/port/provider-configs" {
			t.Errorf("%s valid bounds: no store call recorded (want >= 1)", path)
		}
	}
}

// BenchmarkReferenceListRead is the BEFORE leg: in-process reference
// handler over the fixture store, no transport hop.
func BenchmarkReferenceListRead(b *testing.B) {
	store := shimFixtureStore()
	mux := http.NewServeMux()
	(&config.ReadHandler{Store: store}).Mount(mux)
	authCtx := shimAdminAuth("tenant_a")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/admin/provider-configs", nil)
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d", rec.Code)
		}
	}
}

// BenchmarkShimTerminatedListRead is the AFTER leg: terminated dest
// handler over the loopback port transport (gate + port call +
// projection), same fixtures.
func BenchmarkShimTerminatedListRead(b *testing.B) {
	store := shimFixtureStore()
	portSrv := httptest.NewServer((&shimPortServer{store: store}).handler())
	b.Cleanup(portSrv.Close)
	mux := http.NewServeMux()
	(&shimDestReadHandler{PortBaseURL: portSrv.URL, Client: portSrv.Client()}).Mount(mux)
	authCtx := shimAdminAuth("tenant_a")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/admin/provider-configs", nil)
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d", rec.Code)
		}
	}
}
