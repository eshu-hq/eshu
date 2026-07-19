// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// packageRegistryScopedGraphFake is a GraphQuery test double for the scoped
// package-registry gate tests. Anchor-visibility and version-anchor lookups
// are answered from fixed maps keyed by the anchor id; every other Cypher
// statement (the real packages/versions/dependencies read) is answered from
// a single canned row set. Every statement run is recorded so tests can
// assert the actual predicate reached the store.
type packageRegistryScopedGraphFake struct {
	t                     *testing.T
	visibilityByPackageID map[string]string
	packageIDByVersionID  map[string]string
	rows                  []map[string]any
	calls                 []string
	fatalOnCall           bool
}

func (f *packageRegistryScopedGraphFake) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.fatalOnCall {
		f.t.Fatal("graph store was called despite an empty scoped grant")
	}
	f.calls = append(f.calls, cypher)
	switch cypher {
	case packageRegistryAnchorVisibilityCypher:
		visibility, ok := f.visibilityByPackageID[StringVal(params, "package_id")]
		if !ok {
			return nil, nil
		}
		return []map[string]any{{"visibility": visibility}}, nil
	case packageRegistryVersionAnchorPackageIDCypher:
		packageID, ok := f.packageIDByVersionID[StringVal(params, "version_id")]
		if !ok {
			return nil, nil
		}
		return []map[string]any{{"package_id": packageID}}, nil
	default:
		return f.rows, nil
	}
}

func (f *packageRegistryScopedGraphFake) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// fatalOnCallPackageRegistryCorrelationStore t.Fatal()s on any call, proving
// an empty-grant short-circuit never reaches the correlation probe.
type fatalOnCallPackageRegistryCorrelationStore struct {
	t *testing.T
}

func (s *fatalOnCallPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	context.Context,
	PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	s.t.Fatal("correlation store was called despite an empty scoped grant")
	return nil, nil
}

func tenantAScopedAuthContext() AuthContext {
	return AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}
}

func tenantBScopedAuthContext() AuthContext {
	return AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-b",
		WorkspaceID:          "workspace-b",
		AllowedRepositoryIDs: []string{"repo-b"},
		AllowedScopeIDs:      []string{"scope-b"},
	}
}

// --- packages: two-tenant DATA test ---

// TestPackageRegistryPackagesScopedVisibilityAndGrant is the two-tenant DATA
// test for GET /api/v0/package-registry/packages: a public package is
// visible to any scoped caller with source_path redacted; a private package
// is visible only to the tenant whose correlation grant proves it, with
// source_path intact; an out-of-grant scoped caller sees the SAME empty page
// as a nonexistent package_id (no existence oracle).
func TestPackageRegistryPackagesScopedVisibilityAndGrant(t *testing.T) {
	t.Parallel()

	publicRow := map[string]any{
		"package_id": "pkg:npm:public-lib", "ecosystem": "npm", "normalized_name": "public-lib",
		"source_path": "package.json", "visibility": "public", "version_count": int64(1),
	}
	privateRow := map[string]any{
		"package_id": "pkg:npm:private-lib", "ecosystem": "npm", "normalized_name": "private-lib",
		"source_path": "package.json", "visibility": "private", "version_count": int64(1),
	}

	t.Run("public row visible and redacted for any scoped caller", func(t *testing.T) {
		t.Parallel()
		graph := &packageRegistryScopedGraphFake{
			t:                     t,
			visibilityByPackageID: map[string]string{"pkg:npm:public-lib": "public"},
			rows:                  []map[string]any{publicRow},
		}
		handler := &PackageRegistryHandler{Neo4j: graph, Correlations: &fatalOnCallPackageRegistryCorrelationStore{t: t}, Profile: ProfileProduction}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?package_id=pkg:npm:public-lib&limit=10", nil)
		req = req.WithContext(ContextWithAuthContext(req.Context(), tenantBScopedAuthContext()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "package.json") {
			t.Fatalf("public-branch response leaked source_path: %s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "pkg:npm:public-lib") {
			t.Fatalf("expected public package in response: %s", rec.Body.String())
		}
	})

	t.Run("private row visible without redaction for the granted tenant", func(t *testing.T) {
		t.Parallel()
		graph := &packageRegistryScopedGraphFake{
			t:                     t,
			visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
			rows:                  []map[string]any{privateRow},
		}
		correlations := &recordingPackageRegistryCorrelationStore{
			rows: []PackageRegistryCorrelationRow{{CorrelationID: "corr-a", PackageID: "pkg:npm:private-lib", RepositoryID: "repo-a"}},
		}
		handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?package_id=pkg:npm:private-lib&limit=10", nil)
		req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "package.json") {
			t.Fatalf("granted private row must keep source_path: %s", rec.Body.String())
		}
		if got, want := correlations.lastFilter.PackageID, "pkg:npm:private-lib"; got != want {
			t.Fatalf("correlation probe PackageID = %q, want %q", got, want)
		}
		if got, want := correlations.lastFilter.AllowedRepositoryIDs, []string{"repo-a"}; !equalPacketStringSlices(got, want) {
			t.Fatalf("correlation probe AllowedRepositoryIDs = %#v, want %#v", got, want)
		}
	})

	t.Run("private row invisible to an ungranted tenant, same as nonexistent", func(t *testing.T) {
		t.Parallel()
		graph := &packageRegistryScopedGraphFake{
			t:                     t,
			visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
			rows:                  []map[string]any{privateRow},
		}
		correlations := &recordingPackageRegistryCorrelationStore{rows: nil} // tenant B has no grant row for this package
		handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
		mux := http.NewServeMux()
		handler.Mount(mux)

		grantedReq := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?package_id=pkg:npm:private-lib&limit=10", nil)
		grantedReq = grantedReq.WithContext(ContextWithAuthContext(grantedReq.Context(), tenantBScopedAuthContext()))
		grantedRec := httptest.NewRecorder()
		mux.ServeHTTP(grantedRec, grantedReq)

		nonexistentReq := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?package_id=pkg:npm:does-not-exist&limit=10", nil)
		nonexistentReq = nonexistentReq.WithContext(ContextWithAuthContext(nonexistentReq.Context(), tenantBScopedAuthContext()))
		nonexistentRec := httptest.NewRecorder()
		mux.ServeHTTP(nonexistentRec, nonexistentReq)

		if got, want := grantedRec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, grantedRec.Body.String())
		}
		if strings.Contains(grantedRec.Body.String(), "pkg:npm:private-lib") {
			t.Fatalf("ungranted private response leaked package identity: %s", grantedRec.Body.String())
		}

		var grantedBody, nonexistentBody map[string]any
		if err := json.Unmarshal(grantedRec.Body.Bytes(), &grantedBody); err != nil {
			t.Fatalf("decode granted-case body: %v", err)
		}
		if err := json.Unmarshal(nonexistentRec.Body.Bytes(), &nonexistentBody); err != nil {
			t.Fatalf("decode nonexistent-case body: %v", err)
		}
		if got, want := grantedBody["count"], nonexistentBody["count"]; got != want {
			t.Fatalf("count = %v, want the same as a nonexistent package_id (%v)", got, want)
		}
		if got, want := grantedBody["packages"], nonexistentBody["packages"]; !equalJSONValue(got, want) {
			t.Fatalf("packages = %#v, want the same shape as a nonexistent package_id (%#v)", got, want)
		}
	})
}

// --- packages: empty-grant short circuit ---

func TestPackageRegistryPackagesEmptyGrantReturnsEmptyWithoutAnyStoreRead(t *testing.T) {
	t.Parallel()
	graph := &packageRegistryScopedGraphFake{t: t, fatalOnCall: true}
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: &fatalOnCallPackageRegistryCorrelationStore{t: t}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?package_id=pkg:npm:anything&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := rec.Body.String(), `"count":0`; !strings.Contains(got, want) {
		t.Fatalf("body = %s, want to contain %q", got, want)
	}
}

// --- versions: two-tenant DATA test + empty grant ---

func TestPackageRegistryVersionsScopedVisibilityAndGrant(t *testing.T) {
	t.Parallel()

	versionRow := map[string]any{"version_id": "pv:1", "package_id": "pkg:npm:private-lib", "version": "1.0.0"}

	t.Run("granted tenant sees versions", func(t *testing.T) {
		t.Parallel()
		graph := &packageRegistryScopedGraphFake{
			t:                     t,
			visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
			rows:                  []map[string]any{versionRow},
		}
		correlations := &recordingPackageRegistryCorrelationStore{
			rows: []PackageRegistryCorrelationRow{{CorrelationID: "corr-a", PackageID: "pkg:npm:private-lib", RepositoryID: "repo-a"}},
		}
		handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/versions?package_id=pkg:npm:private-lib&limit=10", nil)
		req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "pv:1") {
			t.Fatalf("expected the granted tenant's version in response: %s", rec.Body.String())
		}
	})

	t.Run("ungranted tenant sees the same empty page as nonexistent package_id", func(t *testing.T) {
		t.Parallel()
		graph := &packageRegistryScopedGraphFake{
			t:                     t,
			visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
			rows:                  []map[string]any{versionRow},
		}
		correlations := &recordingPackageRegistryCorrelationStore{rows: nil}
		handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/versions?package_id=pkg:npm:private-lib&limit=10", nil)
		req = req.WithContext(ContextWithAuthContext(req.Context(), tenantBScopedAuthContext()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "pv:1") {
			t.Fatalf("ungranted response leaked version identity: %s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"count":0`) {
			t.Fatalf("body = %s, want count:0", rec.Body.String())
		}
	})
}

func TestPackageRegistryVersionsEmptyGrantReturnsEmptyWithoutAnyStoreRead(t *testing.T) {
	t.Parallel()
	graph := &packageRegistryScopedGraphFake{t: t, fatalOnCall: true}
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: &fatalOnCallPackageRegistryCorrelationStore{t: t}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/versions?package_id=pkg:npm:anything&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

// --- packages: scoped ecosystem browse ---

// TestPackageRegistryPackagesScopedEcosystemBrowseUsesVisibilityFilteredCypher
// proves the ecosystem-only browse branch (no package_id, no name) sends the
// scoped visibility-filtered Cypher shape, not the plain unscoped shape, and
// redacts source_path on the returned public rows.
func TestPackageRegistryPackagesScopedEcosystemBrowseUsesVisibilityFilteredCypher(t *testing.T) {
	t.Parallel()

	graph := &packageRegistryScopedGraphFake{
		t: t,
		rows: []map[string]any{{
			"package_id": "pkg:npm:public-lib", "ecosystem": "npm", "normalized_name": "public-lib",
			"source_path": "package.json", "visibility": "public", "version_count": int64(1),
		}},
	}
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: &fatalOnCallPackageRegistryCorrelationStore{t: t}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "package.json") {
		t.Fatalf("scoped ecosystem browse leaked source_path: %s", rec.Body.String())
	}
	if len(graph.calls) != 1 || !strings.Contains(graph.calls[0], "p.visibility = 'public'") {
		t.Fatalf("expected the scoped visibility-filtered ecosystem cypher, got calls: %#v", graph.calls)
	}
	if strings.Contains(graph.calls[0], "{ecosystem: $ecosystem}") {
		t.Fatalf("scoped ecosystem cypher must NOT use the inline-pattern+trailing-WHERE shape (NornicDB drops the inline anchor): %s", graph.calls[0])
	}
}

// --- shared/unscoped callers are unaffected ---

func TestPackageRegistryPackagesUnscopedCallerNotRedactedNoGate(t *testing.T) {
	t.Parallel()
	graph := &packageRegistryScopedGraphFake{
		t: t,
		rows: []map[string]any{{
			"package_id": "pkg:npm:private-lib", "ecosystem": "npm", "normalized_name": "private-lib",
			"source_path": "package.json", "visibility": "private", "version_count": int64(1),
		}},
	}
	// A fatal-on-call correlation store proves the probe never runs for an
	// unscoped (shared-key/admin/local) caller.
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: &fatalOnCallPackageRegistryCorrelationStore{t: t}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?package_id=pkg:npm:private-lib&limit=10", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "package.json") {
		t.Fatalf("unscoped caller must see source_path unredacted: %s", rec.Body.String())
	}
	// Exactly one call: the real read. No anchor-visibility pre-check for an
	// unscoped caller (resolvePackageRegistryAnchorGate is a no-op for
	// access.scoped() == false).
	if len(graph.calls) != 1 {
		t.Fatalf("unscoped caller must not trigger the scoped anchor gate, got calls: %#v", graph.calls)
	}
}

// equalJSONValue compares two decoded JSON values (from encoding/json's
// map[string]any/[]any/string/float64/bool/nil universe) for deep equality
// via re-marshaling, avoiding a reflect.DeepEqual mismatch on nil vs empty
// slice that JSON round-tripping already normalizes identically.
func equalJSONValue(a, b any) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aj) == string(bj)
}
