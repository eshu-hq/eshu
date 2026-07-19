// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// countingPackageRegistryCorrelationStore records how many times the
// correlation probe is invoked (and the last filter), so a test can prove the
// nonexistent-anchor and existing-but-gated-anchor cases issue the SAME number
// of correlation round-trips. rows is returned on every call.
type countingPackageRegistryCorrelationStore struct {
	rows       []PackageRegistryCorrelationRow
	calls      int
	lastFilter PackageRegistryCorrelationFilter
}

func (s *countingPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	s.calls++
	s.lastFilter = filter
	return append([]PackageRegistryCorrelationRow(nil), s.rows...), nil
}

// TestPackageRegistryNameBranchNoTimingOracle proves the packages-by-name
// branch issues the SAME number of graph and correlation store round-trips
// whether the name does not resolve at all or resolves to an
// existing-but-ungranted private package, and returns a byte-identical body.
// A caller must not be able to distinguish "package exists" from "package does
// not exist" by round-trip count or latency (no existence oracle). Mutation
// check: reverting the name branch to the asymmetric short-circuit (write
// empty without the sentinel correlation probe) makes the correlation-call
// counts differ (0 vs 1) and this test goes red.
func TestPackageRegistryNameBranchNoTimingOracle(t *testing.T) {
	t.Parallel()

	privateRow := map[string]any{
		"package_id": "pkg:npm:private-lib", "ecosystem": "npm", "normalized_name": "private-lib",
		"visibility": "private", "version_count": int64(1),
	}

	// Existing-but-ungranted: name resolves to a private package tenant B has
	// no grant for.
	existingGraph := &packageRegistryScopedGraphFake{
		t: t,
		nameAnchorByKey: map[string][]map[string]any{
			"npm\x00private-lib": {{"package_id": "pkg:npm:private-lib", "visibility": "private"}},
		},
		visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
		rows:                  []map[string]any{privateRow},
	}
	existingCorr := &countingPackageRegistryCorrelationStore{rows: nil}
	existingBody, existingCode := runScopedPackagesByName(t, existingGraph, existingCorr, "npm", "private-lib")

	// Nonexistent: name does not resolve.
	nonexistentGraph := &packageRegistryScopedGraphFake{
		t:               t,
		nameAnchorByKey: map[string][]map[string]any{}, // no match
		rows:            []map[string]any{privateRow},
	}
	nonexistentCorr := &countingPackageRegistryCorrelationStore{rows: nil}
	nonexistentBody, nonexistentCode := runScopedPackagesByName(t, nonexistentGraph, nonexistentCorr, "npm", "does-not-exist")

	if existingCode != http.StatusOK || nonexistentCode != http.StatusOK {
		t.Fatalf("status codes existing=%d nonexistent=%d, want both 200", existingCode, nonexistentCode)
	}
	if len(existingGraph.calls) != len(nonexistentGraph.calls) {
		t.Fatalf("graph round-trips existing=%d nonexistent=%d, want equal (timing oracle)", len(existingGraph.calls), len(nonexistentGraph.calls))
	}
	if existingCorr.calls != nonexistentCorr.calls {
		t.Fatalf("correlation round-trips existing=%d nonexistent=%d, want equal (timing oracle)", existingCorr.calls, nonexistentCorr.calls)
	}
	if existingCorr.calls != 1 {
		t.Fatalf("correlation calls = %d, want exactly 1 for both cases", existingCorr.calls)
	}
	if existingBody != nonexistentBody {
		t.Fatalf("bodies differ (existence oracle):\n existing=%s\n nonexistent=%s", existingBody, nonexistentBody)
	}
}

// TestPackageRegistryVersionBranchNoTimingOracle is the version_id-anchored
// counterpart: a nonexistent version_id and a version_id resolving to an
// existing-but-ungranted private package must issue the same number of graph
// and correlation round-trips and return a byte-identical body. Mutation: the
// old asymmetric short-circuit made the counts differ (1 graph / 0 corr vs 2
// graph / 1 corr) and this test red.
func TestPackageRegistryVersionBranchNoTimingOracle(t *testing.T) {
	t.Parallel()

	depRow := map[string]any{
		"dependency_id": "dep:1", "source_package_id": "pkg:npm:private-lib", "source_version_id": "pv:1",
		"dependency_package_id": "pkg:npm:leftpad",
	}

	existingGraph := &packageRegistryScopedGraphFake{
		t:                     t,
		packageIDByVersionID:  map[string]string{"pv:1": "pkg:npm:private-lib"},
		visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
		rows:                  []map[string]any{depRow},
	}
	existingCorr := &countingPackageRegistryCorrelationStore{rows: nil}
	existingBody, existingCode := runScopedDependenciesByVersion(t, existingGraph, existingCorr, "pv:1")

	nonexistentGraph := &packageRegistryScopedGraphFake{
		t:                    t,
		packageIDByVersionID: map[string]string{}, // pv:missing does not resolve
		rows:                 []map[string]any{depRow},
	}
	nonexistentCorr := &countingPackageRegistryCorrelationStore{rows: nil}
	nonexistentBody, nonexistentCode := runScopedDependenciesByVersion(t, nonexistentGraph, nonexistentCorr, "pv:missing")

	if existingCode != http.StatusOK || nonexistentCode != http.StatusOK {
		t.Fatalf("status codes existing=%d nonexistent=%d, want both 200", existingCode, nonexistentCode)
	}
	if len(existingGraph.calls) != len(nonexistentGraph.calls) {
		t.Fatalf("graph round-trips existing=%d nonexistent=%d, want equal (timing oracle)", len(existingGraph.calls), len(nonexistentGraph.calls))
	}
	if existingCorr.calls != nonexistentCorr.calls {
		t.Fatalf("correlation round-trips existing=%d nonexistent=%d, want equal (timing oracle)", existingCorr.calls, nonexistentCorr.calls)
	}
	if len(existingGraph.calls) != 2 || existingCorr.calls != 1 {
		t.Fatalf("version branch round-trips = %d graph / %d corr, want 2 graph / 1 corr for both cases", len(existingGraph.calls), existingCorr.calls)
	}
	if existingBody != nonexistentBody {
		t.Fatalf("bodies differ (existence oracle):\n existing=%s\n nonexistent=%s", existingBody, nonexistentBody)
	}
}

func runScopedPackagesByName(
	t *testing.T,
	graph *packageRegistryScopedGraphFake,
	corr PackageRegistryCorrelationStore,
	ecosystem, name string,
) (body string, code int) {
	t.Helper()
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: corr, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem="+ecosystem+"&name="+name+"&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantBScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Body.String(), rec.Code
}

func runScopedDependenciesByVersion(
	t *testing.T,
	graph *packageRegistryScopedGraphFake,
	corr PackageRegistryCorrelationStore,
	versionID string,
) (body string, code int) {
	t.Helper()
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: corr, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependencies?version_id="+versionID+"&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantBScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Body.String(), rec.Code
}
