// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// cutoverOwnershipRelPath is the committed route/mutation-to-contract
// ownership map for eshu-hq/eshu#8872, repo-root-relative so the docs
// lockstep discovery sees this test's read. The tests below keep it honest:
// every live API route must have a row, no row may name a phantom route or
// a missing contract fragment, and every row the map claims newRouter mounts
// must actually resolve on a fully-wired test router.
const cutoverOwnershipRelPath = "go/cmd/api/CUTOVER_OWNERSHIP.md"

// cutoverDocOnlyRoutes are mounted by APIRouter.Mount but intentionally have
// no OpenAPI path entry: the Swagger UI and ReDoc pages serve the spec
// itself rather than versioned read contracts.
var cutoverDocOnlyRoutes = map[string]bool{
	"GET /api/v0/docs":  true,
	"GET /api/v0/redoc": true,
}

// cutoverWireAPIProbeExclusions counts the map rows whose mounted-by column
// is wireAPI. Those handlers (first-run setup, OIDC/SAML/GitHub login,
// provider configs, sign-in policy, ask) are composed in wireAPI after
// newRouter returns, once a live keyring or provider resolver exists, so the
// newRouter-level probe cannot reach them. Their mount correctness is proven
// by their own family tests (cited in the map's proof column); the exact
// count below fails loudly if the excluded set grows silently.
const cutoverWireAPIProbeExclusions = 26

type cutoverMapRow struct {
	route     string
	family    string
	contract  string
	channel   string
	mountedBy string
}

func cutoverRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func readCutoverDoc(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(cutoverRepoRoot(t), cutoverOwnershipRelPath))
	if err != nil {
		t.Fatalf("reading %s: %v, want the committed #8872 ownership map", cutoverOwnershipRelPath, err)
	}
	return string(content)
}

// parseCutoverOwnershipMap reads the committed map's route table. Table rows
// start with "| " and carry METHOD + path in the first cell; header and
// separator lines never parse to a METHOD-prefixed key and are skipped.
func parseCutoverOwnershipMap(t *testing.T) []cutoverMapRow {
	t.Helper()
	content := readCutoverDoc(t)
	var rows []cutoverMapRow
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitCutoverRow(line)
		if len(cells) < 6 {
			continue
		}
		route := strings.Join(strings.Fields(cells[0]), " ")
		route = strings.Trim(route, "`*")
		if !strings.Contains(route, " /") {
			continue
		}
		rows = append(rows, cutoverMapRow{
			route:     route,
			family:    strings.TrimSpace(cells[1]),
			contract:  strings.TrimSpace(cells[2]),
			channel:   strings.TrimSpace(cells[3]),
			mountedBy: strings.TrimSpace(cells[5]),
		})
	}
	if len(rows) == 0 {
		t.Fatalf("%s parses to zero route rows, want the full #8872 route table", cutoverOwnershipRelPath)
	}
	return rows
}

func splitCutoverRow(line string) []string {
	var cells []string
	for _, p := range strings.Split(line, "|") {
		if p = strings.TrimSpace(p); p != "" {
			cells = append(cells, p)
		}
	}
	return cells
}

func cutoverImplementedAPIRoutes(t *testing.T) []string {
	t.Helper()
	inv, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		t.Fatalf("capabilitycatalog.LoadSurfaceInventory() error = %v, want nil", err)
	}
	var routes []string
	for _, s := range inv.Surfaces {
		if s.Category != capabilitycatalog.SurfaceAPIRoute || s.Readiness != capabilitycatalog.ReadinessImplemented {
			continue
		}
		routes = append(routes, s.Name)
	}
	if len(routes) == 0 {
		t.Fatal("surface inventory holds zero implemented api_route surfaces, want the live route census")
	}
	return routes
}

// TestCutoverOwnershipMapCoversEveryRoute is the #8872 acceptance-criterion 1
// lock: the committed map must name every live route and only live routes
// (plus the two doc UIs), and every row's contract fragment must exist.
func TestCutoverOwnershipMapCoversEveryRoute(t *testing.T) {
	t.Parallel()

	rows := parseCutoverOwnershipMap(t)
	live := cutoverImplementedAPIRoutes(t)
	liveSet := make(map[string]bool, len(live))
	for _, r := range live {
		liveSet[r] = true
	}
	seen := make(map[string]string, len(rows))
	for _, r := range rows {
		if prev, dup := seen[r.route]; dup {
			t.Errorf("duplicate map row for %q (families %q and %q)", r.route, prev, r.family)
		}
		seen[r.route] = r.family
	}

	for _, route := range live {
		if _, ok := seen[route]; !ok {
			t.Errorf("implemented route %q has no row in %s", route, cutoverOwnershipRelPath)
		}
	}
	for _, r := range rows {
		if cutoverDocOnlyRoutes[r.route] {
			continue
		}
		if !liveSet[r.route] {
			t.Errorf("map row %q names no implemented api_route surface", r.route)
		}
	}

	root := cutoverRepoRoot(t)
	for _, r := range rows {
		if r.contract == "<mux-health>" || r.contract == "<doc-ui>" {
			continue
		}
		for _, frag := range strings.Fields(r.contract) {
			if _, err := os.Stat(filepath.Join(root, "go", "internal", "query", "openapi", frag)); err != nil {
				t.Errorf("map row %q names missing contract fragment %q", r.route, frag)
			}
		}
		if r.family == "" {
			t.Errorf("map row %q names no APIRouter handler family", r.route)
		}
		if r.channel != "read" && r.channel != "mutation" {
			t.Errorf("map row %q has channel %q, want read or mutation", r.route, r.channel)
		}
		if r.mountedBy != "newRouter" && r.mountedBy != "wireAPI" {
			t.Errorf("map row %q has mounted-by %q, want newRouter or wireAPI", r.route, r.mountedBy)
		}
	}
}

var cutoverPathParam = regexp.MustCompile(`\{[^}]*\}`)

// cutoverConcretePath renders an inventory route path ("POST " prefix
// stripped) into a concrete request path: every {param} or {rest...} segment
// becomes "x", which the Go 1.22 ServeMux still matches to the pattern.
func cutoverConcretePath(route string) string {
	_, path, _ := strings.Cut(route, " ")
	return cutoverPathParam.ReplaceAllString(path, "x")
}

// TestCutoverNewRouterMountsMappedRoutes probes every newRouter-mounted spec
// route with an OPTIONS request: a mounted path answers 405 without executing
// the handler, while an unmounted path answers 404. Probing with a wrong
// method proves registration only, so never-dialed test backends and empty
// bodies cannot fail the assertion.
func TestCutoverNewRouterMountsMappedRoutes(t *testing.T) {
	t.Parallel()

	rows := parseCutoverOwnershipMap(t)
	router := newFullyWiredTestRouter(t)
	mux := http.NewServeMux()
	router.Mount(mux)

	excluded := 0
	probed := 0
	for _, r := range rows {
		if cutoverDocOnlyRoutes[r.route] {
			continue
		}
		if r.mountedBy == "wireAPI" {
			excluded++
			continue
		}
		req := httptest.NewRequest(http.MethodOptions, cutoverConcretePath(r.route), nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		probed++
		if w.Code == http.StatusNotFound {
			t.Errorf("OPTIONS %s -> 404: map claims newRouter mounts %q but the mux has no such path", cutoverConcretePath(r.route), r.route)
		}
	}
	if probed == 0 {
		t.Fatal("probe covered zero routes, want every newRouter map row")
	}
	if excluded != cutoverWireAPIProbeExclusions {
		t.Errorf("wireAPI exclusions = %d, want %d: an excluded route changed mounting without map review", excluded, cutoverWireAPIProbeExclusions)
	}
}
