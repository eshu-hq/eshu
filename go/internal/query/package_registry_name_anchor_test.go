// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// keyedPackageRegistryCorrelationStore answers ListPackageRegistryCorrelations
// per package_id from a fixed grant/deny map, for tests that need different
// candidates to resolve differently through the correlation-grant probe.
type keyedPackageRegistryCorrelationStore struct {
	grantedPackageIDs map[string]bool
}

func (s *keyedPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	if s.grantedPackageIDs[filter.PackageID] {
		return []PackageRegistryCorrelationRow{{PackageID: filter.PackageID}}, nil
	}
	return nil, nil
}

// TestPackageRegistryPackagesByNamePreservesEveryGrantedCandidate is the
// regression test for the F-6/W5 review finding: normalized_name is not a
// unique package identity within an ecosystem (distinct registries or
// namespaces can share it), so the packages-by-name scoped branch must not
// collapse the {ecosystem, normalized_name} anchor to a single resolved
// package_id. Before the fix, packageRegistryPackagesGate resolved only the
// FIRST matching row from packageRegistryNameAnchorVisibilityCypher and
// re-anchored the real read on that one uid, silently dropping every other
// package sharing the name -- public or privately grant-accessible alike,
// with the survivor depending on arbitrary backend row order.
//
// Three candidates share {ecosystem: "npm", normalized_name: "collide"}:
//   - pkg-public: visibility=public, must always be visible (redacted
//     source_path).
//   - pkg-granted: visibility=private, tenant A holds a correlation grant for
//     it, must be visible (unredacted source_path).
//   - pkg-denied: visibility=private, tenant A holds no grant for it, must be
//     excluded entirely.
func TestPackageRegistryPackagesByNamePreservesEveryGrantedCandidate(t *testing.T) {
	t.Parallel()

	const (
		pkgPublic  = "pkg:npm:collide-public"
		pkgGranted = "pkg:npm:collide-granted"
		pkgDenied  = "pkg:npm:collide-denied"
	)

	graph := &packageRegistryScopedGraphFake{
		t: t,
		nameAnchorByKey: map[string][]map[string]any{
			"npm\x00collide": {
				{"package_id": pkgPublic, "visibility": "public"},
				{"package_id": pkgGranted, "visibility": "private"},
				{"package_id": pkgDenied, "visibility": "private"},
			},
		},
		rows: []map[string]any{
			{
				"package_id": pkgPublic, "ecosystem": "npm", "normalized_name": "collide",
				"source_path": "public/package.json", "visibility": "public",
			},
			{
				"package_id": pkgGranted, "ecosystem": "npm", "normalized_name": "collide",
				"source_path": "granted/package.json", "visibility": "private",
			},
			{
				"package_id": pkgDenied, "ecosystem": "npm", "normalized_name": "collide",
				"source_path": "denied/package.json", "visibility": "private",
			},
		},
	}
	correlations := &keyedPackageRegistryCorrelationStore{grantedPackageIDs: map[string]bool{pkgGranted: true}}
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&name=collide&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var body struct {
		Packages []PackageRegistryPackageResult `json:"packages"`
		Count    int                            `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rec.Body.String())
	}
	byID := make(map[string]PackageRegistryPackageResult, len(body.Packages))
	for _, pkg := range body.Packages {
		byID[pkg.PackageID] = pkg
	}
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2 (public + granted-private, denied-private excluded); packages = %#v", body.Count, body.Packages)
	}
	if _, ok := byID[pkgDenied]; ok {
		t.Fatalf("ungranted candidate %s leaked into the response: %#v", pkgDenied, body.Packages)
	}
	public, ok := byID[pkgPublic]
	if !ok {
		t.Fatalf("public candidate %s missing from response (the exact bug: collapsing to one candidate drops siblings): %#v", pkgPublic, body.Packages)
	}
	if public.SourcePath != "" {
		t.Fatalf("public candidate source_path = %q, want redacted", public.SourcePath)
	}
	granted, ok := byID[pkgGranted]
	if !ok {
		t.Fatalf("grant-accessible private candidate %s missing from response: %#v", pkgGranted, body.Packages)
	}
	if granted.SourcePath != "granted/package.json" {
		t.Fatalf("grant-accessible candidate source_path = %q, want unredacted", granted.SourcePath)
	}
}
