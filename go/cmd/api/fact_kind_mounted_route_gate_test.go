// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// factKindRegistrySpecPath resolves the committed
// specs/fact-kind-registry.v1.yaml path from this test file's location
// (cmd/api -> cmd -> go -> repo root -> specs), mirroring
// readSurfaceGateSpecsDir in go/internal/mcp/read_surface_consumer_existence_test.go.
func factKindRegistrySpecPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "specs", "fact-kind-registry.v1.yaml"))
}

// TestFactKindMountedRouteGateCatchesDocumentedButUnmountedRoute is the
// codex-review P1 fix on #5335/#5359: the family half of
// TestFactKindRegistryReadSurfacesResolveToLiveRoutes
// (go/internal/mcp/read_surface_consumer_existence_test.go) only ever checked
// each read_surface literal against
// capabilitycatalog.LoadSurfaceInventory -- the DOCUMENTED surface inventory
// derived from the served OpenAPI spec (query.OpenAPISpec() by way of
// cmd/capability-inventory's enumerateAPIRoutes) -- never against the set of
// routes actually registered on the production *http.ServeMux
// (query.APIRouter.Mount). A route can be declared in an
// openapi_paths_*.go source file (so it appears in the spec, and so in the
// inventory that gate consults) while the handler that would serve it is
// never wired into APIRouter -- verify-openapi.sh keeps the spec in parity
// with HandleFunc *declarations*, not with what newRouter actually mounts --
// so a caller following the documented route gets a live 404 while the old
// gate stayed green: exactly the "advertised capability with no real
// consumer" defect class #5335 exists to catch, just one layer further down
// the stack than the ledger claims it was designed for.
//
// This test proves the new mounted-route check (factKindReadSurfaceMounted)
// actually closes that gap, by reproducing the defect on purpose: it takes a
// real fact-kind read_surface route ("GET /api/v0/ci-cd/run-correlations",
// owned solely by CICDHandler.Mount), confirms it is still present in the
// documented surface inventory (so the OLD gate would have passed it), then
// builds the real production router with router.CICD deliberately left nil
// -- APIRouter.Mount skips a nil handler entirely -- and shows
// factKindReadSurfaceMounted now reports it unmounted. Before
// factKindReadSurfaceMounted existed, nothing in this package could catch
// that; only a check against the real mounted mux can.
func TestFactKindMountedRouteGateCatchesDocumentedButUnmountedRoute(t *testing.T) {
	t.Parallel()

	const surface = "GET /api/v0/ci-cd/run-correlations"

	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		t.Fatalf("capabilitycatalog.LoadSurfaceInventory() error = %v", err)
	}
	documented := false
	for _, s := range inventory.Surfaces {
		if s.Category == capabilitycatalog.SurfaceAPIRoute && s.Readiness == capabilitycatalog.ReadinessImplemented && s.Name == surface {
			documented = true
			break
		}
	}
	if !documented {
		t.Fatalf("test setup invalid: %q is no longer in the documented surface inventory; pick a different fact-kind read_surface route owned by exactly one handler", surface)
	}

	router := newFullyWiredTestRouter(t)
	router.CICD = nil // simulate the defect class: declared/documented, never mounted.
	mux := http.NewServeMux()
	router.Mount(mux)

	mounted, _, err := factKindReadSurfaceMounted(mux, surface)
	if err != nil {
		t.Fatalf("factKindReadSurfaceMounted(%q) error = %v", surface, err)
	}
	if mounted {
		t.Fatalf("factKindReadSurfaceMounted(%q) = true, want false: router.CICD was deliberately left nil so APIRouter.Mount would skip it -- the gate must detect a documented-but-unmounted route, not just echo the documented inventory", surface)
	}
}

// TestFactKindReadSurfacesAreActuallyMountedOnRealRouter is the #5359
// mounted-route-parity half of the #5335 GATE 1 fact-kind check. Every
// family-level read_surface literal in specs/fact-kind-registry.v1.yaml
// (excluding "none") must resolve to a route registered on the real,
// production-wired *http.ServeMux (newFullyWiredTestRouter + APIRouter.Mount)
// -- not merely appear in the documented surface inventory, which
// TestFactKindRegistryReadSurfacesResolveToLiveRoutes already checks in
// go/internal/mcp. Riding the same credential-free `go test ./cmd/api` floor
// as every other Go package test; no separate workflow.
//
// Scope note: this reuses newFullyWiredTestRouter's construction, which wires
// everything newRouter wires but not the two routes wireAPI (cmd/api/wiring.go)
// mounts directly on the outer apiMux (POST /api/v0/ask,
// serviceintelhttp.ReportHandler) -- see routerFieldsNotWiredByNewRouter's
// "Ask" entry. No fact-kind read_surface names either route today (verified
// against the 17 literals this test iterates), so that residual gap does not
// currently affect this gate's coverage; a future read_surface pointed at one
// of those two routes would need this test extended to mount them too.
func TestFactKindReadSurfacesAreActuallyMountedOnRealRouter(t *testing.T) {
	t.Parallel()

	surfaces, err := mcp.LoadFactKindRegistryReadSurfaces(factKindRegistrySpecPath(t))
	if err != nil {
		t.Fatalf("LoadFactKindRegistryReadSurfaces: %v", err)
	}

	router := newFullyWiredTestRouter(t)
	mux := http.NewServeMux()
	router.Mount(mux)

	families := make([]string, 0, len(surfaces))
	for family := range surfaces {
		families = append(families, family)
	}
	sort.Strings(families)

	for _, family := range families {
		surface := surfaces[family]
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			mounted, _, err := factKindReadSurfaceMounted(mux, surface)
			if err != nil {
				t.Fatalf("factKindReadSurfaceMounted(%q) error = %v", surface, err)
			}
			if !mounted {
				t.Errorf("family %q read_surface %q is documented but NOT mounted on the real API router -- advertised-but-unservable (#5359)", family, surface)
			}
		})
	}
}
