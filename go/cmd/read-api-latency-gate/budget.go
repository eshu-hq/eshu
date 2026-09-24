// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// RouteLatency is one route's outcome from a sweep.
type RouteLatency struct {
	// Route is "METHOD /path", matching the capabilitycatalog surface
	// inventory Name field for an api_route entry.
	Route string
	// P95 is the measured p95 latency. Meaningful only when Exercised is
	// true; a not-exercised route was never sampled past its first probe.
	P95 time.Duration
	// Exercised is false when the route's first (and only) probe returned a
	// 4xx: this gate could not run the route's real query (a required
	// selector was missing, or auth scope was insufficient), so the fast
	// rejection is not latency evidence and must not count as a pass. A 2xx,
	// a 5xx, or a client-side timeout all count as exercised — the backend
	// was actually reached.
	Exercised bool
	// Status is the last observed HTTP status code (0 for a timeout, which
	// has no response).
	Status int
	// HardFailed is true when any sample for this route returned a 5xx. A
	// 5xx must never pass the gate just because it answered fast — see
	// EvaluateBudgets.
	HardFailed bool
	// HardFailedBody is the first ~hardFailedBodyCap bytes of the response
	// body from the FIRST counted-sample 5xx, when HardFailed is true. Empty
	// otherwise. This is the error envelope an operator needs to root-cause
	// a HardFailed route without re-running the gate (issue #6797 live-gate
	// incident: two HardFailed routes had no signal beyond "5xx" until this
	// was added).
	HardFailedBody string
	// Metered is true when the work meter read counters for this route; Work is
	// meaningful only then. See WorkPerRequest.
	Metered bool
	Work    WorkPerRequest
	// Samples holds the COLD (first) run's counted-iteration sample
	// durations, in request order. Populated whenever Exercised is true,
	// regardless of SweepOptions.Runs. P95 is computed from these samples
	// when Runs <= 1 (the default), so this is the same sample set that has
	// always backed the gate's own budget check.
	Samples []time.Duration
	// WarmSamples holds every counted sample from runs 2..Runs, pooled in
	// run order (empty when Runs <= 1). Run 1's cold connection and cold
	// Postgres/NornicDB caches make it unrepresentative of steady state
	// (the same reasoning as warmupRequests, one level up); WarmSamples is
	// what a multi-run latency report's distribution is computed from, and
	// what P95 is computed from when Runs > 1.
	WarmSamples []time.Duration
	// WarmRunP95s holds the nearest-rank p95 of EACH individual warm run
	// (runs 2..Runs), in run order — not the p95 of the pooled WarmSamples.
	// Its min..max shows run-to-run spread that a single pooled p95 cannot:
	// a host under variable load can produce a stable pooled p95 while
	// individual runs swing widely.
	WarmRunP95s []time.Duration
}

// BudgetBreach is one route that failed the gate: either its measured p95
// exceeded Budget, or HardFailed is true (a 5xx sample), in which case P95
// may be well under Budget and is not the reason for the breach.
type BudgetBreach struct {
	Route          string
	P95            time.Duration
	Budget         time.Duration
	HardFailed     bool
	HardFailedBody string
}

// catalogCIMultiplier scales a capability's committed production p95 budget
// before comparing it against the TSV/default budget. The committed p95 is
// measured (or targeted) on production hardware; this gate runs on a shared
// CI runner against a synthetic, not-yet-load-tested corpus, so a 1.5x
// allowance keeps a real production budget from flaking a CI run over
// ordinary shared-runner variance. Matches the identical 1.5x headroom
// documented in testdata/benchmarks/reducer-handler-budgets.txt for the same
// reason (a runner-class difference between where a budget's baseline was
// captured and where it is enforced).
const catalogCIMultiplier = 1.5

// RouteCapability maps a no-arg GET route to its capability id in the
// capability catalog (specs/capability-matrix.v1.yaml, embedded as
// go/internal/capabilitycatalog/data/catalog.generated.json). This is a
// manually-curated, test-verified subset (TestCatalogBudgetWinsWhenTighter,
// TestConfiguredBudgetWinsWhenCatalogLooser), not an exhaustive generated
// mapping — no such mapping is derivable at build time today (the capability
// matrix's "tools" field is an MCP/API tool name that only sometimes equals
// the HTTP route string, e.g. "GET /api/v0/status/operations" but also
// "list_iac_resources"; the actual route<->capability association lives only
// in each handler's own `XCapability = "..."` Go constant, verified here by
// reading go/internal/query source directly). A route absent from this map
// simply skips the catalog step and falls through to the TSV table/default.
var RouteCapability = map[string]string{
	// go/internal/query/infra_resource_aggregates_handler.go:
	// infraResourceAggregateCapability, shared by both handlers.
	"GET /api/v0/infra/resources/count":     "platform_impact.infra_resource_aggregate",
	"GET /api/v0/infra/resources/inventory": "platform_impact.infra_resource_aggregate",
	// go/internal/query/iac/resources.go: ResourcesCapability.
	"GET /api/v0/iac/resources": "iac_inventory.resources.list",
	// go/internal/query/status.go: the capability-matrix tool string for
	// operations.status is literally "GET /api/v0/status/operations".
	"GET /api/v0/status/operations": "operations.status",
	// go/internal/query/capability/handler.go-family: capability_catalog.list's
	// tool string is literally the route itself.
	"GET /api/v0/capabilities": "capability_catalog.list",
}

// catalogProductionP95 returns capability's declared production p95 latency
// budget from catalog, and whether one is declared at all (a capability may
// have no production profile, or a profile with no p95_latency_ms).
func catalogProductionP95(catalog capabilitycatalog.Catalog, capability string) (time.Duration, bool) {
	for _, entry := range catalog.Entries {
		if entry.Capability != capability {
			continue
		}
		profile, ok := entry.Profiles["production"]
		if !ok || profile.P95LatencyMS == nil {
			return 0, false
		}
		return time.Duration(*profile.P95LatencyMS) * time.Millisecond, true
	}
	return 0, false
}

// routeBudgetRow is one parsed non-default row: a budget plus the required
// justification for declaring it explicitly instead of relying on the
// default.
type routeBudgetRow struct {
	budget time.Duration
	reason string
}

// RouteBudgets holds the parsed per-route latency budgets, the required
// default, and (once WithCatalog is called) the capability catalog used to
// tighten a route's budget when RouteCapability maps it to a capability with
// a stricter declared production p95.
type RouteBudgets struct {
	byRoute map[string]routeBudgetRow
	def     time.Duration
	catalog capabilitycatalog.Catalog
	haveCat bool
}

// WithCatalog returns a copy of b that also consults catalog: for any route
// RouteCapability maps to a capability with a declared production p95,
// For reports min(catalog p95 * catalogCIMultiplier, the TSV/default value)
// — the catalog value only ever tightens the effective budget, never loosens
// it silently.
func (b RouteBudgets) WithCatalog(catalog capabilitycatalog.Catalog) RouteBudgets {
	b.catalog = catalog
	b.haveCat = true
	return b
}

// For returns the effective budget for route: the tighter of the declared
// TSV/default budget and (when RouteCapability maps the route and a catalog
// was attached via WithCatalog) the catalog's production p95 scaled by
// catalogCIMultiplier.
func (b RouteBudgets) For(route string) time.Duration {
	configured := b.def
	if row, ok := b.byRoute[route]; ok {
		configured = row.budget
	}

	if !b.haveCat {
		return configured
	}
	capability, ok := RouteCapability[route]
	if !ok {
		return configured
	}
	catalogP95, ok := catalogProductionP95(b.catalog, capability)
	if !ok {
		return configured
	}
	catalogAdjusted := time.Duration(float64(catalogP95) * catalogCIMultiplier)
	if catalogAdjusted < configured {
		return catalogAdjusted
	}
	return configured
}

// ParseRouteBudgets reads the tab-separated budget table:
//
//   - blank lines and lines starting with "#" are ignored.
//   - "default\t<budget-ms>" is required exactly once and applies to any GET
//     route the table does not name explicitly, so a newly added route
//     defaults to a sane budget instead of silently going unchecked
//     (issue #6797).
//   - every other line is "<route>\t<budget-ms>\t<reason>": the reason is
//     required so an explicit override is always reviewable (why this route
//     needs its own number instead of the default, or why it overrides a
//     looser/absent catalog budget).
//   - a route named more than once is an error: a later row silently
//     replacing an earlier one is exactly the kind of override this format
//     exists to make visible instead of hiding.
func ParseRouteBudgets(r io.Reader) (RouteBudgets, error) {
	budgets := RouteBudgets{byRoute: map[string]routeBudgetRow{}}
	haveDefault := false

	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			return RouteBudgets{}, fmt.Errorf("route budget line %d: expected \"<route>\\t<budget-ms>[\\t<reason>]\", got %q", lineNo, line)
		}
		key := strings.TrimSpace(fields[0])
		msText := strings.TrimSpace(fields[1])
		ms, err := strconv.Atoi(msText)
		if err != nil {
			return RouteBudgets{}, fmt.Errorf("route budget line %d: non-numeric budget %q: %w", lineNo, msText, err)
		}
		budget := time.Duration(ms) * time.Millisecond

		if key == "default" {
			if haveDefault {
				return RouteBudgets{}, fmt.Errorf("route budget line %d: duplicate \"default\" row; a later row must not silently override an earlier one", lineNo)
			}
			budgets.def = budget
			haveDefault = true
			continue
		}

		if len(fields) < 3 || strings.TrimSpace(fields[2]) == "" {
			return RouteBudgets{}, fmt.Errorf("route budget line %d: route %q requires a third \\t-separated reason column", lineNo, key)
		}
		if _, dup := budgets.byRoute[key]; dup {
			return RouteBudgets{}, fmt.Errorf("route budget line %d: duplicate route %q; a later row must not silently override an earlier one", lineNo, key)
		}
		budgets.byRoute[key] = routeBudgetRow{budget: budget, reason: strings.TrimSpace(fields[2])}
	}
	if err := scanner.Err(); err != nil {
		return RouteBudgets{}, fmt.Errorf("read route budgets: %w", err)
	}
	if !haveDefault {
		return RouteBudgets{}, fmt.Errorf("route budget table missing required \"default\" row")
	}

	return budgets, nil
}

// EvaluateBudgets compares each measured result against its budget and
// returns one BudgetBreach per route whose p95 exceeded it, in results order.
// A route with Exercised=false carries no latency evidence and is skipped —
// coverage of not-exercised routes is enforced separately (see
// ExercisedCoverage and RequireNamedRoutesExercised), not folded into a
// budget breach. A route with HardFailed=true (any 5xx sample) is always a
// breach regardless of its measured p95 — a fast failure must not pass.
func EvaluateBudgets(results []RouteLatency, budgets RouteBudgets) []BudgetBreach {
	var breaches []BudgetBreach
	for _, r := range results {
		if !r.Exercised {
			continue
		}
		budget := budgets.For(r.Route)
		if r.HardFailed {
			breaches = append(breaches, BudgetBreach{Route: r.Route, P95: r.P95, Budget: budget, HardFailed: true, HardFailedBody: r.HardFailedBody})
			continue
		}
		if r.P95 > budget {
			// An exempt route's ceiling is advisory (LatencyExemptions), so
			// it never reaches the breach list here -- only its HardFailed
			// and work-budget checks can still fail the run.
			if _, exempt := LatencyExemptions[r.Route]; exempt {
				continue
			}
			breaches = append(breaches, BudgetBreach{
				Route:  r.Route,
				P95:    r.P95,
				Budget: budget,
			})
		}
	}
	return breaches
}

// ExemptedLatencyBreaches returns one BudgetBreach per exercised,
// non-HardFailed route whose p95 exceeded its budget but is covered by
// LatencyExemptions. EvaluateBudgets excludes these from the run's failures;
// this is the reporting counterpart so the run's stderr still names them
// instead of going silent about a route that is, in fact, over its ceiling.
func ExemptedLatencyBreaches(results []RouteLatency, budgets RouteBudgets) []BudgetBreach {
	var exempted []BudgetBreach
	for _, r := range results {
		if !r.Exercised || r.HardFailed {
			continue
		}
		budget := budgets.For(r.Route)
		if r.P95 <= budget {
			continue
		}
		if _, exempt := LatencyExemptions[r.Route]; exempt {
			exempted = append(exempted, BudgetBreach{Route: r.Route, P95: r.P95, Budget: budget})
		}
	}
	return exempted
}

// RequireNamedRoutesExercised returns every route explicitly named in
// budgets' table (not merely covered by the default) that came back
// Exercised=false in results. A route this table names has been declared
// important enough to carry its own budget; if it silently stops being
// exercisable (a renamed route, a broken auth scope, a newly-required
// selector), that must fail the gate loudly instead of quietly dropping the
// route from coverage while the gate stays green.
// A route with no explicit row is unaffected — it is allowed to be
// not-exercised, subject only to the overall ExercisedCoverageFloor.
func RequireNamedRoutesExercised(results []RouteLatency, budgets RouteBudgets) []string {
	var missing []string
	for _, r := range results {
		if r.Exercised {
			continue
		}
		if _, named := budgets.byRoute[r.Route]; named {
			missing = append(missing, r.Route)
		}
	}
	return missing
}
