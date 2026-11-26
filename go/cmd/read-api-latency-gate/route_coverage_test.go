// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// TestRouteCoverageTableRoutesToItsDeclaredLabels is the GREEN half of the hermetic coverage: each
// declared request routes, by the stores' own pure functions, to exactly its declared labels, and
// every label the impact package lists has a request (legacy aside).
func TestRouteCoverageTableRoutesToItsDeclaredLabels(t *testing.T) {
	t.Parallel()

	if problems := routeCoverageProblems(routeCoverageRows(), impact.AllReadRoutes()); len(problems) != 0 {
		t.Fatalf("route coverage table problems:\n- %s", strings.Join(problems, "\n- "))
	}
}

// TestRouteCoverageCatchesPlantedViolations is the seeded RED half: a new label with no request,
// a request whose filter routes elsewhere, and a request declaring the legacy route are reported.
func TestRouteCoverageCatchesPlantedViolations(t *testing.T) {
	t.Parallel()

	rows := routeCoverageRows()
	t.Run("a new label with no request", func(t *testing.T) {
		labels := append(impact.AllReadRoutes(), impact.ReadRoute("planted_new_route"))
		problems := routeCoverageProblems(rows, labels)
		if len(problems) != 1 || !strings.Contains(problems[0], "planted_new_route") {
			t.Fatalf("problems = %v, want exactly the missing label", problems)
		}
	})
	t.Run("a request whose filter routes elsewhere", func(t *testing.T) {
		planted := append([]routeCoverageRow(nil), rows...)
		planted[0].Profile = impact.ProfilePrecise // routes to winners_supplement, declared winners
		problems := routeCoverageProblems(planted, impact.AllReadRoutes())
		if len(problems) == 0 {
			t.Fatal("a request that routes to another label was accepted")
		}
	})
	t.Run("a request declaring legacy", func(t *testing.T) {
		planted := append([]routeCoverageRow(nil), rows...)
		planted[0].Declared = []impact.ReadRoute{impact.RouteLegacy}
		if problems := routeCoverageProblems(planted, impact.AllReadRoutes()); len(problems) == 0 {
			t.Fatal("a request declaring the legacy route was accepted")
		}
	})
	t.Run("a dropped request leaves its label uncovered", func(t *testing.T) {
		if problems := routeCoverageProblems(rows[1:], impact.AllReadRoutes()); len(problems) == 0 {
			t.Fatal("dropping the winners request left no problem")
		}
	})
}

const plantedScrape = `# HELP eshu_dp_supply_chain_impact_read_route_total reads by route
# TYPE eshu_dp_supply_chain_impact_read_route_total counter
eshu_dp_supply_chain_impact_read_route_total{otel_scope_name="x",route="winners"} 7
eshu_dp_supply_chain_impact_read_route_total{otel_scope_name="y",route="winners"} 3
eshu_dp_supply_chain_impact_read_route_total{route="supports_image"} 2
some_other_metric{route="winners"} 99
`

// TestParseRouteCounter pins the scrape parser: it sums series that differ only in other labels,
// ignores other metrics and comments, and reads an absent series as zero.
func TestParseRouteCounter(t *testing.T) {
	t.Parallel()

	got := parseRouteCounter(plantedScrape)
	if got["winners"] != 10 || got["supports_image"] != 2 || len(got) != 2 {
		t.Fatalf("parseRouteCounter = %v, want winners 10 and supports_image 2 only", got)
	}
	if got["supports_grant"] != 0 {
		t.Fatalf("an absent series reads %v, want 0", got["supports_grant"])
	}
}

// TestRouteRiseComparator is the RED/GREEN pair of the live comparator over planted scrapes: a clean
// rise on the declared label passes, one declared label at zero fails, and a rise on an undeclared
// label fails; an absent series reads as zero both ways.
func TestRouteRiseComparator(t *testing.T) {
	t.Parallel()

	declared := []impact.ReadRoute{impact.RouteSupportsFindingID, impact.RouteSupportsSelector}
	before := map[string]float64{"winners": 5}
	clean := map[string]float64{"winners": 5, "supports_finding_id": 1, "supports_selector": 2}
	if problems := routeRiseProblems("miss", before, clean, declared); len(problems) != 0 {
		t.Fatalf("a clean scrape was rejected: %v", problems)
	}
	oneAtZero := map[string]float64{"winners": 5, "supports_finding_id": 1}
	if problems := routeRiseProblems("miss", before, oneAtZero, declared); len(problems) != 1 || !strings.Contains(problems[0], "supports_selector") {
		t.Fatalf("a declared label at zero gave %v, want exactly that label", problems)
	}
	undeclared := map[string]float64{"winners": 6, "supports_finding_id": 1, "supports_selector": 1}
	if problems := routeRiseProblems("miss", before, undeclared, declared); len(problems) != 1 || !strings.Contains(problems[0], "winners") {
		t.Fatalf("an undeclared rise gave %v, want exactly winners", problems)
	}
	if problems := legacyRouteProblems(map[string]float64{}, map[string]float64{"legacy": 1}); len(problems) != 1 {
		t.Fatalf("a legacy rise gave %v, want one problem", problems)
	}
	if problems := legacyRouteProblems(map[string]float64{"legacy": 4}, map[string]float64{"legacy": 4}); len(problems) != 0 {
		t.Fatalf("a flat legacy count gave %v, want none", problems)
	}
}

// fakeCore serves the impact paths and a metrics endpoint whose route counter rises as chosen by
// labelFor, so the live runner can be exercised without a core.
type fakeCore struct {
	mu       sync.Mutex
	counts   map[string]float64
	labelFor func(r *http.Request) []string
}

func (f *fakeCore) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for label, value := range f.counts {
			_, _ = fmt.Fprintf(w, "%s{route=%q} %g\n", impactReadRouteCounter, label, value)
		}
	})
	mux.HandleFunc("/api/v0/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		for _, label := range f.labelFor(r) {
			f.counts[label]++
		}
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// labelsForRow answers, for a request, the labels the real route functions give: the fake core is
// as right as the declared table, so a pass here proves the runner, not the table.
func labelsForRow(rows []routeCoverageRow) func(r *http.Request) []string {
	return func(r *http.Request) []string {
		for _, row := range rows {
			// The granted and the winners requests share a path; the caller's key tells them apart.
			grantedCaller := strings.Contains(r.Header.Get("Authorization"), "granted-key") || strings.Contains(r.Header.Get("X-API-Key"), "granted-key")
			if row.Path == r.URL.RequestURI() && (row.Caller == callerGranted) == grantedCaller {
				out := make([]string, 0, len(row.Declared))
				for _, label := range row.Declared {
					out = append(out, string(label))
				}
				return out
			}
		}
		return nil
	}
}

func coverageOptions(server *httptest.Server, granted string) RouteCoverageOptions {
	return RouteCoverageOptions{
		BaseURL: server.URL, MetricsURL: server.URL + "/metrics", APIKey: "admin", GrantedAPIKey: granted,
		Iterations: 1, Runs: 1, Timeout: 5 * time.Second,
	}
}

// TestSweepRouteCoverageAgainstAFakeCore runs the live runner over a fake core: a core whose label
// rises match the declared table passes, a core that raises the wrong label fails, a missing
// granted key fails, and a legacy rise fails.
func TestSweepRouteCoverageAgainstAFakeCore(t *testing.T) {
	t.Parallel()

	rows := routeCoverageRows()
	t.Run("clean", func(t *testing.T) {
		t.Parallel()
		core := &fakeCore{counts: map[string]float64{}, labelFor: labelsForRow(rows)}
		server := httptest.NewServer(core.handler())
		defer server.Close()
		results, problems, err := SweepRouteCoverage(coverageOptions(server, "granted-key"))
		if err != nil {
			t.Fatalf("SweepRouteCoverage() error = %v", err)
		}
		if len(problems) != 0 {
			t.Fatalf("a clean core reported problems: %v", problems)
		}
		if len(results) != len(rows) {
			t.Fatalf("results = %d, want one latency row per request (%d)", len(results), len(rows))
		}
	})
	t.Run("a core that raises the wrong label", func(t *testing.T) {
		t.Parallel()
		core := &fakeCore{counts: map[string]float64{}, labelFor: func(r *http.Request) []string { return []string{"winners"} }}
		server := httptest.NewServer(core.handler())
		defer server.Close()
		_, problems, err := SweepRouteCoverage(coverageOptions(server, "granted-key"))
		if err != nil {
			t.Fatalf("SweepRouteCoverage() error = %v", err)
		}
		if len(problems) == 0 {
			t.Fatal("a core that raises only winners passed the coverage")
		}
	})
	t.Run("no granted key", func(t *testing.T) {
		t.Parallel()
		core := &fakeCore{counts: map[string]float64{}, labelFor: labelsForRow(rows)}
		server := httptest.NewServer(core.handler())
		defer server.Close()
		_, problems, err := SweepRouteCoverage(coverageOptions(server, ""))
		if err != nil {
			t.Fatalf("SweepRouteCoverage() error = %v", err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], "granted") {
			t.Fatalf("a missing granted key gave %v, want exactly the granted-key problem", problems)
		}
	})
	t.Run("a legacy rise", func(t *testing.T) {
		t.Parallel()
		clean := labelsForRow(rows)
		core := &fakeCore{counts: map[string]float64{}, labelFor: func(r *http.Request) []string { return append(clean(r), "legacy") }}
		server := httptest.NewServer(core.handler())
		defer server.Close()
		_, problems, err := SweepRouteCoverage(coverageOptions(server, "granted-key"))
		if err != nil {
			t.Fatalf("SweepRouteCoverage() error = %v", err)
		}
		if len(problems) == 0 {
			t.Fatal("a legacy rise passed the coverage")
		}
	})
}
