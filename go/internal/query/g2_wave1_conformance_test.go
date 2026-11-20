// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// G2 Wave-1 seven-leg replication, leg 1: conformance (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). TDD record: the
// frozen table below failed RED before g2_wave1_wave.go landed
// (g2Wave1 undefined), and the method-judgment assertions failed next
// (g2Wave1MethodAllowed undefined in g2_wave1_terminate.go); both are
// green with the wave table plus the lane rule. Reference only —
// every line here is new core-side code (oracle discipline).
//
// Core-side reading of the W8a conformance legs: the 9 wave rows are
// explicitly registered on the mux (no catch-all serves them), the 5
// GET base legs terminate through their admission pins, the 4 POST
// rider spellings are judged 405 GET-only by the method-qualified
// patterns before any admission is touched, and every trailing-slash
// twin stays 404 (never-split: a twin never serves a different
// answer).

// TestG2Wave1Frozen pins the exact surface Wave 1 proves: the 5 G2
// base legs behind the three v0 pins plus the 4 forwarded-read rider
// spellings from contracts/step2-frozen-scope.md (R4-R7). A wave
// change moves the ownership note in g2_wave1_wave.go, the oracle
// fragment column, and this mirror together.
func TestG2Wave1Frozen(t *testing.T) {
	t.Parallel()

	want := []string{
		"GET /api/v0/collector-extraction-readiness ExtractionReadiness native paths/status/collector_extraction_readiness.go",
		"GET /api/v0/collector-extraction-readiness/{family} ExtractionReadiness native paths/status/collector_extraction_readiness.go",
		"GET /api/v0/fact-schema-versions FactSchemaVersions native paths/status/fact_schema_version.go",
		"GET /api/v0/fact-schema-versions/{fact_kind} FactSchemaVersions native paths/status/fact_schema_version.go",
		"GET /api/v0/surface-inventory SurfaceInventory native paths/catalog/surface_inventory.go",
		"POST /api/v0/freshness/changed-since Freshness native paths/freshness/changed_since.go",
		"POST /api/v0/freshness/generations Freshness native paths/freshness/generations.go",
		"POST /api/v0/freshness/services/changed-since Freshness native paths/freshness/service_changed_since.go",
		"POST /api/v0/surface-inventory SurfaceInventory native paths/catalog/surface_inventory.go",
	}
	var got []string
	for _, entry := range g2Wave1 {
		got = append(got, entry.Method+" "+entry.Path+" "+entry.Family+" "+entry.Serve+" "+entry.Fragment)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("g2Wave1 holds %d entries, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("g2Wave1 differs at index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// g2Wave1CannedMux mounts the Wave-1 handlers on a bare mux with
// hermetic canned admissions, so the conformance legs prove explicit
// registration (not a catch-all) and pin admission (not the static
// registries). Freshness rides a zero Handler: Wave 1 only POSTs to
// freshness paths, which the mux judges before any reader runs.
func g2Wave1CannedMux() *http.ServeMux {
	mux := http.NewServeMux()
	router := &APIRouter{
		ExtractionReadiness: &CollectorExtractionReadinessHandler{
			Profile: ProfileProduction,
			Reads: &cannedExtractionReadiness{rows: map[scope.CollectorKind]extraction.Readiness{
				scope.CollectorGit: {Family: scope.CollectorGit, DisplayName: "Canned Git"},
			}},
		},
		FactSchemaVersions: &FactSchemaVersionHandler{
			Profile: ProfileProduction,
			Reads:   cannedFactSchema{},
		},
		SurfaceInventory: &SurfaceInventoryHandler{Profile: ProfileProduction},
		Freshness:        &FreshnessHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)
	return mux
}

// TestG2Wave1NativeMount pins that every wave row resolves on an
// explicitly mounted route: the 5 GET base legs serve (never 404 or
// 405) and the 4 POST rider spellings are judged 405 with an
// Allow header naming the pinned GET method.
func TestG2Wave1NativeMount(t *testing.T) {
	t.Parallel()

	mux := g2Wave1CannedMux()
	for _, entry := range g2Wave1 {
		t.Run(entry.Method+" "+entry.Path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			var req *http.Request
			if entry.Method == http.MethodPost {
				req = httptest.NewRequest(http.MethodPost, entry.Path, strings.NewReader(`{}`))
			} else {
				req = httptest.NewRequest(http.MethodGet, concretizeG2Wave1Path(entry.Path), nil)
			}
			mux.ServeHTTP(rec, req)

			if entry.Method == http.MethodPost {
				if rec.Code != http.StatusMethodNotAllowed {
					t.Fatalf("%s %s: status = %d, want 405 (core judges the method GET-only)", entry.Method, entry.Path, rec.Code)
				}
				if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
					t.Errorf("%s %s: Allow = %q, want it to name the pinned GET method", entry.Method, entry.Path, allow)
				}
				return
			}
			if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s: status = %d, want the natively mounted leg (never 404/405)", entry.Method, entry.Path, rec.Code)
			}
		})
	}
}

// TestG2Wave1MethodJudgmentMatchesPredicate pins the lane rule
// against the mux: the g2Wave1MethodAllowed predicate (the stated
// "core allowlists each route to its pinned method" rule) agrees
// with the served judgment for every wave row, and stays closed for
// a non-wave spelling.
func TestG2Wave1MethodJudgmentMatchesPredicate(t *testing.T) {
	t.Parallel()

	mux := g2Wave1CannedMux()
	for _, entry := range g2Wave1 {
		t.Run(entry.Method+" "+entry.Path, func(t *testing.T) {
			t.Parallel()

			if !g2Wave1MethodAllowed(entry.Method, entry.Path) {
				t.Fatalf("g2Wave1MethodAllowed(%s %s) = false, want the wave row allowed", entry.Method, entry.Path)
			}
			// No cross-method rejection here: /api/v0/surface-inventory
			// is frozen twice (GET base leg + POST rider, per R7), so
			// the predicate holds both spellings and the mux judges
			// each (serve vs 405, pinned in TestG2Wave1NativeMount).
		})
	}
	for _, spelled := range []struct{ method, path string }{
		{http.MethodPatch, "/api/v0/collector-extraction-readiness"},
		{http.MethodDelete, "/api/v0/surface-inventory"},
		{http.MethodPost, "/api/v0/freshness/bogus"},
	} {
		if g2Wave1MethodAllowed(spelled.method, spelled.path) {
			t.Errorf("g2Wave1MethodAllowed(%s %s) = true, want false (not a wave row)", spelled.method, spelled.path)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(spelled.method, spelled.path, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("%s %s: status = 200, want the non-wave spelling closed", spelled.method, spelled.path)
		}
	}
}

// TestG2Wave1SlashSpellingsNeverSplit pins the never-split rule for
// the wave: every trailing-slash spelling stays 404 (the twin never
// serves a different answer) with zero admission contact.
func TestG2Wave1SlashSpellingsNeverSplit(t *testing.T) {
	t.Parallel()

	counting := &countingExtractionReadiness{inner: &cannedExtractionReadiness{rows: map[scope.CollectorKind]extraction.Readiness{
		scope.CollectorGit: {Family: scope.CollectorGit, DisplayName: "Canned Git"},
	}}}
	mux := http.NewServeMux()
	(&APIRouter{
		ExtractionReadiness: &CollectorExtractionReadinessHandler{Profile: ProfileProduction, Reads: counting},
		FactSchemaVersions:  &FactSchemaVersionHandler{Profile: ProfileProduction, Reads: cannedFactSchema{}},
		SurfaceInventory:    &SurfaceInventoryHandler{Profile: ProfileProduction},
		Freshness:           &FreshnessHandler{Profile: ProfileProduction},
	}).Mount(mux)

	for _, entry := range g2Wave1 {
		t.Run(entry.Method+" "+entry.Path+" slash", func(t *testing.T) {
			t.Parallel()

			var req *http.Request
			if entry.Method == http.MethodPost {
				req = httptest.NewRequest(http.MethodPost, entry.Path+"/", strings.NewReader(`{}`))
			} else {
				req = httptest.NewRequest(http.MethodGet, concretizeG2Wave1Path(entry.Path)+"/", nil)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s/: status = %d, want 404 (twins never split)", entry.Method, entry.Path, rec.Code)
			}
		})
	}
	if counting.catalogCalls+counting.lookupCalls != 0 {
		t.Errorf("slash twins touched the admission %d times, want zero contact", counting.catalogCalls+counting.lookupCalls)
	}
}

// TestG2Wave1UnknownRouteFailClosed pins the fail-closed default:
// any route the wave table does not know answers 404 and never rides
// a leg.
func TestG2Wave1UnknownRouteFailClosed(t *testing.T) {
	t.Parallel()

	mux := g2Wave1CannedMux()
	for _, target := range []string{
		"/api/v0/collector-extraction-readiness/git/extra",
		"/api/v0/fact-schema-versions/kind/extra",
		"/api/v0/freshness/bogus",
		"/api/v0/surface-inventory/extra",
	} {
		t.Run("GET "+target, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s: status = %d, want the fail-closed 404", target, rec.Code)
			}
			body, _ := io.ReadAll(rec.Result().Body)
			if len(body) == 0 {
				t.Errorf("GET %s: empty body, want the documented 404 page", target)
			}
		})
	}
}

// TestG2Wave1OtherFreshnessPathsNotInWave pins the wave boundary:
// the freshness-path reads owned by other lanes keep their families
// and never enter this wave.
func TestG2Wave1OtherFreshnessPathsNotInWave(t *testing.T) {
	t.Parallel()

	others := []string{
		"GET /api/v0/repositories/repo_1/freshness",
		"GET /api/v0/documentation/evidence-packets/pkt_1/freshness",
		"GET /api/v0/status/freshness-causality",
	}
	for _, other := range others {
		if g2Wave1MethodAllowed(strings.SplitN(other, " ", 2)[0], strings.SplitN(other, " ", 2)[1]) {
			t.Errorf("earlier-lane route %q is in g2Wave1, want it out", other)
		}
		for _, entry := range g2Wave1 {
			if entry.Method+" "+entry.Path == other {
				t.Errorf("earlier-lane route %q is a g2Wave1 row, want it out", other)
			}
		}
	}
}

// concretizeG2Wave1Path swaps a templated wave path for a canned row
// the hermetic admissions track, so mount-proof requests resolve.
func concretizeG2Wave1Path(path string) string {
	path = strings.ReplaceAll(path, "{family}", string(scope.CollectorGit))
	path = strings.ReplaceAll(path, "{fact_kind}", "canned-fact")
	return path
}
