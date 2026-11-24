// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// The route coverage of the impact reads (#10880 PR-C2 n, arbiter-c2-n-armed section 1). The
// impact store answers a call from one of several read models and counts the one it took on
// eshu_dp_supply_chain_impact_read_route_total{route}. The generic sweep runs one unscoped call
// per route, so it reaches only some labels; this table declares, per label, the one sweep
// request that reaches it. The hermetic half (routeCoverageProblems) calls the same pure route
// functions the stores use and requires each request to route to its declared label; the live
// half (SweepRouteCoverage) scrapes the counter around each request and requires a rise of at
// least one on the declared label and none on any other.

// Callers a coverage request runs as.
const (
	callerAdmin   = "admin"
	callerGranted = "granted"
)

// routeCoverageRow is one declared sweep request.
type routeCoverageRow struct {
	// Name is the row's name in the latency and work reports.
	Name string
	// Path is the request path and query on the core API.
	Path string
	// Caller is callerAdmin (the gate's own key) or callerGranted (a key with a repository grant).
	Caller string
	// Explain marks a call to the explain route, which reads a public-id probe and, on a miss,
	// a selector read; every other row is a list call.
	Explain bool
	// Miss marks an explain whose public-id probe misses, so both statements run.
	Miss bool
	// SelectorOnly marks an explain with no finding id: no public-id probe runs, only the
	// selector read.
	SelectorOnly bool
	// ImageRef and Profile are the filter inputs of the route function.
	ImageRef, Profile string
	// Declared is the labels the request must raise by at least one, and the only ones it may.
	Declared []impact.ReadRoute
}

// The seeded corpus the requests name: group 0 of the winners seed (see seed_winners.go).
const (
	coverageFindingID = "finding:winners-seed-00000"
	coverageCVE       = "CVE-2026-00000"
	coveragePackage   = "pkg:npm/seed-0000@1.0.0"
	coverageImageRef  = winnersCoverageImageRef
)

const (
	coverageFindingsPath = "/api/v0/supply-chain/impact/findings"
	coverageExplainPath  = "/api/v0/supply-chain/impact/explain"
)

// routeCoverageRows returns the declared request of every route label the gate can reach.
func routeCoverageRows() []routeCoverageRow {
	return []routeCoverageRow{
		{Name: "route:winners", Path: coverageFindingsPath + "?profile=comprehensive&limit=1", Caller: callerAdmin, Profile: impact.ProfileComprehensive, Declared: []impact.ReadRoute{impact.RouteWinners}},
		{Name: "route:winners_supplement", Path: coverageFindingsPath + "?profile=precise&limit=1", Caller: callerAdmin, Profile: impact.ProfilePrecise, Declared: []impact.ReadRoute{impact.RouteWinnersSupplement}},
		{Name: "route:supports_image", Path: coverageFindingsPath + "?profile=comprehensive&limit=1&image_ref=" + coverageImageRef, Caller: callerAdmin, ImageRef: coverageImageRef, Profile: impact.ProfileComprehensive, Declared: []impact.ReadRoute{impact.RouteSupportsImage}},
		{Name: "route:supports_grant", Path: coverageFindingsPath + "?profile=comprehensive&limit=1", Caller: callerGranted, Profile: impact.ProfileComprehensive, Declared: []impact.ReadRoute{impact.RouteSupportsGrant}},
		{Name: "route:supports_finding_id", Path: coverageExplainPath + "?finding_id=" + coverageFindingID, Caller: callerAdmin, Explain: true, Declared: []impact.ReadRoute{impact.RouteSupportsFindingID}},
		{Name: "route:supports_selector", Path: coverageExplainPath + "?cve_id=" + coverageCVE + "&package_id=" + coveragePackage, Caller: callerAdmin, Explain: true, SelectorOnly: true, Declared: []impact.ReadRoute{impact.RouteSupportsSelector}},
		{Name: "route:supports_finding_id_miss", Path: coverageExplainPath + "?finding_id=finding:no-such-finding", Caller: callerAdmin, Explain: true, Miss: true, Declared: []impact.ReadRoute{impact.RouteSupportsFindingID, impact.RouteSupportsSelector}},
	}
}

// routeCoverageProblems checks rows against the real route functions and labels: every row routes
// to exactly its declared labels, every label in labels except legacy has at least one row, and
// no row declares legacy (the live sweep asserts that label rises by zero over the whole run).
func routeCoverageProblems(rows []routeCoverageRow, labels []impact.ReadRoute) []string {
	var problems []string
	covered := map[impact.ReadRoute]bool{}
	for _, row := range rows {
		granted := row.Caller == callerGranted
		var want []impact.ReadRoute
		if row.Explain {
			publicID, selector := impact.ExplainRoutesForCall(true, granted, row.ImageRef)
			switch {
			case row.SelectorOnly:
				want = []impact.ReadRoute{selector}
			case row.Miss:
				want = []impact.ReadRoute{publicID, selector}
			default:
				want = []impact.ReadRoute{publicID}
			}
		} else {
			want = []impact.ReadRoute{impact.RouteForCall(true, granted, row.ImageRef, row.Profile)}
		}
		if !sameRoutes(want, row.Declared) {
			problems = append(problems, fmt.Sprintf("request %s declares %v but the route functions answer %v for its filter", row.Name, row.Declared, want))
		}
		for _, label := range row.Declared {
			if label == impact.RouteLegacy {
				problems = append(problems, fmt.Sprintf("request %s declares the legacy route: the live sweep asserts it never rises", row.Name))
			}
			covered[label] = true
		}
		if row.Path == "" || row.Name == "" {
			problems = append(problems, "a coverage request has no name or path")
		}
	}
	for _, label := range labels {
		if label != impact.RouteLegacy && !covered[label] {
			problems = append(problems, fmt.Sprintf("route label %q has no declared sweep request: add one to routeCoverageRows", label))
		}
	}
	return problems
}

func sameRoutes(a, b []impact.ReadRoute) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[impact.ReadRoute]int{}
	for _, route := range a {
		seen[route]++
	}
	for _, route := range b {
		seen[route]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// operation returns the sweep Operation of the row: a plain GET with no expectation, since the
// label rise, not the body, is the assertion.
func (row routeCoverageRow) operation() Operation {
	return Operation{Method: http.MethodGet, Path: row.Path}
}
