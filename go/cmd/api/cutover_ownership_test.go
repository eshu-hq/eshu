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
// provider configs, sign-in policy, ask, plus the five storeless 8874-S4
// CLI-support routes mounted beside serviceintelhttp) are composed in wireAPI
// after newRouter returns, once a live keyring or provider resolver exists,
// so the newRouter-level probe cannot reach them. Their mount correctness is
// proven by their own family tests (cited in the map's proof column); the
// exact count below fails loudly if the excluded set grows silently.
const cutoverWireAPIProbeExclusions = 31

type cutoverMapRow struct {
	route       string
	family      string
	contract    string
	channel     string
	persistence string
	mountedBy   string
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
			route:       route,
			family:      strings.TrimSpace(cells[1]),
			contract:    strings.TrimSpace(cells[2]),
			channel:     strings.TrimSpace(cells[3]),
			persistence: strings.TrimSpace(cells[4]),
			mountedBy:   strings.TrimSpace(cells[5]),
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

// cutoverPersistenceVocabulary is the exact persistence-column wording the
// #8872 ownership-map review approved. A new store, a reworded cell, or a
// new channel/persistence pairing fails here and forces map review, so the
// extraction cannot silently gain a writer-handle touchpoint.
var cutoverPersistenceVocabulary = map[string]bool{
	"reader (ReadStore/graph/content, no durable writes)":                            true,
	"reader (StatusReader/ReadStore; writer handle held for governance audit reads)": true,
	"writer: identity/session/token stores":                                          true,
	"writer: Postgres recovery/reindex/dead-letter/queue stores":                     true,
	"writer: login-state/session stores":                                             true,
	"writer: provider-config stores":                                                 true,
	"writer: identity stores (roles, IdP mappings)":                                  true,
	"writer: browser-session store":                                                  true,
	"writer: bootstrap stores":                                                       true,
	"writer: terminal-failure ack store + replay ledger":                             true,
	"writer: sign-in-policy store":                                                   true,
	"writer: retirement marker/fencer/purger stores":                                 true,
	"writer: VulnerabilitySuppressionMutationStore (suppressions only)":              true,
	"none (pure function of the request body; no store reads or writes)":             true,
	"n/a (mux-level)": true,
}

// cutoverReadRowsWithWriterStores pins how many read-channel rows are built
// with a writer-capable store handle (status reads over recovery stores,
// session reads over session stores, suppression reads over the suppression
// store). A new one means a new extraction hazard and needs map review.
const cutoverReadRowsWithWriterStores = 32

// cutoverReadRowsHoldingWriterHandle pins how many read-channel rows hold a
// writer handle for governance audit reads. A new one needs map review.
const cutoverReadRowsHoldingWriterHandle = 18

// cutoverMuxLevelRows pins how many rows use n/a (mux-level) persistence
// (liveness probe, spec browsers, served spec document). A new one means a
// route without an owning store and needs map review.
const cutoverMuxLevelRows = 4

// TestCutoverOwnershipPersistencePairingsAreReviewed is the #8872
// acceptance-criterion 1 lock for the persistence half of the map: every row
// must use reviewed persistence wording, every mutation must name the
// core-owned writer store it persists through, and the set of read rows that
// touch writer handles must not grow without review.
func TestCutoverOwnershipPersistencePairingsAreReviewed(t *testing.T) {
	t.Parallel()

	rows := parseCutoverOwnershipMap(t)
	writerStoreReads := 0
	writerHandleReads := 0
	muxLevelRows := 0
	for _, r := range rows {
		if !cutoverPersistenceVocabulary[r.persistence] {
			t.Errorf("map row %q has persistence %q, want a reviewed vocabulary value", r.route, r.persistence)
		}
		switch r.channel {
		case "mutation":
			if !strings.HasPrefix(r.persistence, "writer:") {
				t.Errorf("mutation row %q has persistence %q, want the core-owned writer store", r.route, r.persistence)
			}
		case "read":
			switch {
			case strings.HasPrefix(r.persistence, "writer:"):
				writerStoreReads++
			case strings.Contains(r.persistence, "writer handle held"):
				writerHandleReads++
			}
		}
		if r.persistence == "n/a (mux-level)" {
			muxLevelRows++
			if r.family != "APIRouter" || r.channel != "read" {
				t.Errorf("map row %q is n/a (mux-level) with family %q channel %q, want APIRouter read", r.route, r.family, r.channel)
			}
		}
	}
	if writerStoreReads != cutoverReadRowsWithWriterStores {
		t.Errorf("read rows with writer stores = %d, want %d: a read route gained or lost a writer store without map review", writerStoreReads, cutoverReadRowsWithWriterStores)
	}
	if writerHandleReads != cutoverReadRowsHoldingWriterHandle {
		t.Errorf("read rows holding a writer handle = %d, want %d: a read route gained or lost a writer handle without map review", writerHandleReads, cutoverReadRowsHoldingWriterHandle)
	}
	if muxLevelRows != cutoverMuxLevelRows {
		t.Errorf("mux-level rows = %d, want %d: a route gained or lost n/a persistence without map review", muxLevelRows, cutoverMuxLevelRows)
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
