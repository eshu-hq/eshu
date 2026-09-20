// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	postgresDSN := flag.String("postgres-dsn", envOr("ESHU_POSTGRES_DSN", ""), "Postgres DSN to seed and read status/readiness routes against")
	graphURI := flag.String("graph-uri", envOr("NEO4J_URI", "bolt://localhost:7687"), "NornicDB/Neo4j bolt URI to seed infra label nodes into")
	graphDatabase := flag.String("graph-database", envOr("DEFAULT_DATABASE", "nornic"), "graph database name")
	graphUsername := flag.String("graph-username", os.Getenv("NEO4J_USERNAME"), "graph username (empty for no-auth backends)")
	graphPassword := flag.String("graph-password", os.Getenv("NEO4J_PASSWORD"), "graph password")
	apiBaseURL := flag.String("api-base-url", envOr("READ_API_LATENCY_GATE_BASE_URL", "http://localhost:18080"), "running eshu-api base URL")
	apiKey := flag.String("api-key", os.Getenv("ESHU_API_KEY"), "eshu-api bearer token")
	totalScopes := flag.Int("total-scopes", 800, "total ingestion_scopes rows to seed")
	nodesPerLabel := flag.Int("nodes-per-label", 150000, "synthetic graph nodes to seed per infra label")
	iacFactCount := flag.Int("iac-fact-count", 150000, "IaC content_entity fact_records rows to seed (issue #6793: currentInventoryCTE jsonb detoast cost)")
	iterations := flag.Int("iterations", 20, "requests per route for the p95 sweep")
	budgetsPath := flag.String("budgets", "testdata/benchmarks/read-api-route-budgets.txt", "route latency budget table path")
	workBudgetsPath := flag.String("work-budgets", "testdata/benchmarks/read-api-route-work-budgets.txt", "route Postgres work budget table path")
	workReportPath := flag.String("work-report", "", "write the per-route Postgres work measured this run as JSON (input to scripts/refresh-read-api-work-budgets.sh)")
	backgroundIdle := flag.Duration("background-idle", 5*time.Second, "idle window used to measure background Postgres statements before the sweep")
	skipSeed := flag.Bool("skip-seed", false, "skip Postgres+graph seeding and sweep an already-seeded database")
	seedOnly := flag.Bool("seed-only", false, "seed Postgres+graph, verify the counts, and exit without sweeping (eshu-api starts after this, so its startup backfill sees the seeded content)")
	requestTimeout := flag.Duration("request-timeout", 30*time.Second, "per-request timeout for the sweep")
	flag.Parse()

	if err := run(runOptions{
		postgresDSN:     *postgresDSN,
		graphURI:        *graphURI,
		graphDatabase:   *graphDatabase,
		graphUsername:   *graphUsername,
		graphPassword:   *graphPassword,
		apiBaseURL:      *apiBaseURL,
		apiKey:          *apiKey,
		totalScopes:     *totalScopes,
		nodesPerLabel:   *nodesPerLabel,
		iacFactCount:    *iacFactCount,
		iterations:      *iterations,
		budgetsPath:     *budgetsPath,
		workBudgetsPath: *workBudgetsPath,
		workReportPath:  *workReportPath,
		backgroundIdle:  *backgroundIdle,
		skipSeed:        *skipSeed,
		seedOnly:        *seedOnly,
		requestTimeout:  *requestTimeout,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "read-api-latency-gate:", err)
		os.Exit(1)
	}
}

type runOptions struct {
	postgresDSN     string
	graphURI        string
	graphDatabase   string
	graphUsername   string
	graphPassword   string
	apiBaseURL      string
	apiKey          string
	totalScopes     int
	nodesPerLabel   int
	iacFactCount    int
	iterations      int
	budgetsPath     string
	workBudgetsPath string
	workReportPath  string
	backgroundIdle  time.Duration
	skipSeed        bool
	seedOnly        bool
	requestTimeout  time.Duration
}

func run(opts runOptions) error {
	ctx := context.Background()

	if err := ValidateLatencyExemptions(LatencyExemptions); err != nil {
		return fmt.Errorf("latency exemptions: %w", err)
	}
	if opts.postgresDSN == "" {
		return fmt.Errorf("postgres-dsn (or ESHU_POSTGRES_DSN) is required to seed and to meter Postgres work")
	}
	if opts.seedOnly && opts.skipSeed {
		return fmt.Errorf("-seed-only and -skip-seed are mutually exclusive")
	}
	if !opts.skipSeed {
		if err := seed(ctx, opts); err != nil {
			return err
		}
	}
	if opts.seedOnly {
		fmt.Fprintln(os.Stderr, "read-api-latency-gate: seed complete (-seed-only); not sweeping")
		return nil
	}

	readModelInstalled, err := awaitInfraReadModel(ctx, opts.postgresDSN, os.Stderr)
	if err != nil {
		return err
	}
	if readModelInstalled {
		if err := assertInfraServedFromReadModel(ctx, opts.apiBaseURL, opts.apiKey, opts.requestTimeout, os.Stderr); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "read-api-latency-gate: infra routes report truth.basis hybrid/content_index (served from the read model)")
	}

	meter, closeMeter, err := prepareWorkMeter(ctx, opts.postgresDSN, opts.backgroundIdle, os.Stderr)
	if err != nil {
		return err
	}
	defer closeMeter()

	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		return fmt.Errorf("load surface inventory: %w", err)
	}
	routes := NoArgGetRoutes(inventory)
	if len(routes) == 0 {
		return fmt.Errorf("no-arg GET routes: found none in the surface inventory")
	}
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: sweeping %d no-arg GET routes x %d iterations\n", len(routes), opts.iterations)

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    opts.apiBaseURL,
		APIKey:     opts.apiKey,
		Routes:     routes,
		QueryArgs:  RouteQueryArgs,
		Iterations: opts.iterations,
		Timeout:    opts.requestTimeout,
		Meter:      meter,
		Context:    ctx,
	})
	if err != nil {
		return fmt.Errorf("sweep routes: %w", err)
	}
	if err := writeWorkReportFile(opts.workReportPath, results); err != nil {
		return err
	}

	budgetsTable, err := readTable(opts.budgetsPath)
	if err != nil {
		return fmt.Errorf("read budgets file %s: %w", opts.budgetsPath, err)
	}
	budgets, err := ParseRouteBudgets(budgetsTable.reader())
	if err != nil {
		return fmt.Errorf("parse budgets file %s: %w", opts.budgetsPath, err)
	}
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		return fmt.Errorf("load capability catalog: %w", err)
	}
	budgets = budgets.WithCatalog(catalog)

	workBudgetsTable, err := readTable(opts.workBudgetsPath)
	if err != nil {
		return fmt.Errorf("read work budgets file %s: %w", opts.workBudgetsPath, err)
	}
	workBudgets, err := ParseRouteWorkBudgets(workBudgetsTable.reader())
	if err != nil {
		return fmt.Errorf("parse work budgets file %s: %w", opts.workBudgetsPath, err)
	}
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: latency budgets %s\n", budgetsTable.provenance())
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: work budgets %s\n", workBudgetsTable.provenance())

	printReport(os.Stdout, results, budgets, workBudgets)

	exercised, total := ExercisedCoverage(results)
	fmt.Fprintf(os.Stderr, "\nread-api-latency-gate: exercised %d/%d routes (floor %d)\n", exercised, total, ExercisedCoverageFloor)

	var failures []string

	if breaches := EvaluateBudgets(results, budgets); len(breaches) > 0 {
		fmt.Fprintf(os.Stderr, "\nread-api-latency-gate: %d route(s) exceeded budget:\n", len(breaches))
		for _, b := range breaches {
			if b.HardFailed {
				fmt.Fprintf(os.Stderr, "  %s: 5xx observed (p95 %s, budget %s) -- HardFailed always breaches regardless of latency\n", b.Route, b.P95, b.Budget)
				if b.HardFailedBody != "" {
					fmt.Fprintf(os.Stderr, "      body: %s\n", b.HardFailedBody)
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "  %s: p95 %s > budget %s\n", b.Route, b.P95, b.Budget)
		}
		failures = append(failures, fmt.Sprintf("%d route(s) exceeded their latency budget", len(breaches)))
	}

	if exempted := ExemptedLatencyBreaches(results, budgets); len(exempted) > 0 {
		fmt.Fprintf(os.Stderr, "\nread-api-latency-gate: %d route(s) exceeded their latency budget but are exempt (advisory only):\n", len(exempted))
		for _, b := range exempted {
			ex := LatencyExemptions[b.Route]
			fmt.Fprintf(os.Stderr, "  %s: p95 %s > budget %s -- %s: %s\n", b.Route, b.P95, b.Budget, ex.Issue, ex.Reason)
		}
	}

	failures = append(failures, reportWorkResults(os.Stderr, results, workBudgets)...)

	if coverageErr := CheckCoverageFloor(results, ExercisedCoverageFloor); coverageErr != nil {
		fmt.Fprintf(os.Stderr, "read-api-latency-gate: %v\n", coverageErr)
		failures = append(failures, coverageErr.Error())
	}

	if missing := RequireNamedRoutesExercised(results, budgets); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "read-api-latency-gate: %d explicitly-budgeted route(s) were not exercised: %s\n", len(missing), strings.Join(missing, ", "))
		failures = append(failures, fmt.Sprintf("%d explicitly-budgeted route(s) were not exercised", len(missing)))
	}

	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: all %d exercised routes within budget\n", exercised)
	return nil
}

// seed connects to Postgres and the graph backend and seeds both from a
// freshly built SeedPlan.
func seed(ctx context.Context, opts runOptions) error {
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: opts.totalScopes})
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d scopes, %d generations, %d work items\n",
		len(plan.Scopes), countGenerations(plan), countWorkItems(plan))

	pool, err := pgxpool.New(ctx, opts.postgresDSN)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()
	if err := SeedPostgres(ctx, pool, plan); err != nil {
		return fmt.Errorf("seed postgres: %w", err)
	}

	iacScope, ok := firstScopeOfKind(plan, scope.CollectorTerraformState)
	if !ok {
		return fmt.Errorf("seed plan has no %s-kind scope to anchor IaC facts on", scope.CollectorTerraformState)
	}
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d IaC content_entity facts on scope %s\n", opts.iacFactCount, iacScope.ScopeID)
	iacFacts := BuildIaCFacts(iacScope.ScopeID, iacScope.ActiveGenerationID, opts.iacFactCount)
	if err := SeedIaCFacts(ctx, pool, iacFacts, time.Now().UTC()); err != nil {
		return fmt.Errorf("seed IaC facts: %w", err)
	}

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d graph-only facts per label on scope %s (#6843: the fact truth the read model serves instead of the whole-label graph scan)\n", opts.nodesPerLabel, iacScope.ScopeID)
	graphOnlyFacts := BuildGraphOnlyFacts(iacScope.ScopeID, iacScope.ActiveGenerationID, opts.nodesPerLabel)
	if err := SeedGraphOnlyFacts(ctx, pool, graphOnlyFacts, time.Now().UTC()); err != nil {
		return fmt.Errorf("seed graph-only facts: %w", err)
	}

	expectedCounts := expectedRelationalCounts(plan, iacFacts)
	expectedCounts["fact_records"] += len(graphOnlyFacts)
	if err := VerifyRelationalCounts(ctx, pool, expectedCounts); err != nil {
		return fmt.Errorf("verify seeded Postgres tables: %w", err)
	}

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d nodes per infra label (%d labels)\n", opts.nodesPerLabel, len(infraLabels))
	graphOpts := SeedGraphOptions{
		URI:           opts.graphURI,
		Username:      opts.graphUsername,
		Password:      opts.graphPassword,
		DatabaseName:  opts.graphDatabase,
		NodesPerLabel: opts.nodesPerLabel,
	}
	if err := SeedGraph(ctx, graphOpts); err != nil {
		return fmt.Errorf("seed graph: %w", err)
	}

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d correlated IaC graph nodes (uid = Postgres entity_id)\n", len(iacFacts))
	if err := SeedIaCGraphNodes(ctx, graphOpts, iacFacts); err != nil {
		return fmt.Errorf("seed IaC graph nodes: %w", err)
	}

	if err := VerifyGraphNodeCounts(ctx, graphOpts, expectedGraphNodeCounts(opts.nodesPerLabel, iacFacts)); err != nil {
		return fmt.Errorf("verify seeded graph: %w", err)
	}
	if err := VerifyGraphDimensions(ctx, graphOpts); err != nil {
		return fmt.Errorf("verify seeded graph: %w", err)
	}

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding content_entities rows mirroring the %d content-derived infra labels and the IaC nodes\n", len(contentDerivedInfraLabels()))
	if err := SeedInfraContentEntities(ctx, pool, opts.nodesPerLabel, iacFacts, time.Now().UTC()); err != nil {
		return fmt.Errorf("seed infra content_entities: %w", err)
	}
	if err := MarkOwnerLedgerBackfillComplete(ctx, pool, time.Now().UTC()); err != nil {
		return err
	}
	if err := VerifyContentEntityCounts(ctx, pool, expectedContentEntityCounts(opts.nodesPerLabel, iacFacts)); err != nil {
		return fmt.Errorf("verify seeded content_entities: %w", err)
	}

	fmt.Fprintln(os.Stderr, "read-api-latency-gate: ANALYZE seeded Postgres tables")
	if err := analyzeSeededTables(ctx, pool); err != nil {
		return fmt.Errorf("analyze seeded tables: %w", err)
	}

	return nil
}

// analyzeSeededTables runs ANALYZE on every table this gate bulk-writes, so
// the sweep queries plan against fresh statistics instead of whatever
// autovacuum happened to have computed (or not) at seed time — a source of
// run-to-run plan and latency variance a COPY-then-immediately-sweep gate
// would otherwise carry silently.
func analyzeSeededTables(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, "ANALYZE ingestion_scopes, scope_generations, fact_work_items, fact_records, content_entities")
	return err
}

// firstScopeOfKind returns the first scope in plan.Scopes with the given
// collector kind, in plan.Scopes' (deterministic) order.
func firstScopeOfKind(plan SeedPlan, kind scope.CollectorKind) (SeedScope, bool) {
	for _, s := range plan.Scopes {
		if s.CollectorKind == kind {
			return s, true
		}
	}
	return SeedScope{}, false
}

func countGenerations(plan SeedPlan) int {
	n := 0
	for _, gens := range plan.GenerationsByScope {
		n += len(gens)
	}
	return n
}

func countWorkItems(plan SeedPlan) int {
	n := 0
	for _, items := range plan.WorkItemsByGeneration {
		n += len(items)
	}
	return n
}

func printReport(w io.Writer, results []RouteLatency, budgets RouteBudgets, workBudgets RouteWorkBudgets) {
	workBreached := make(map[string]bool)
	for _, b := range EvaluateWorkBudgets(results, workBudgets) {
		workBreached[b.Route] = true
	}
	for _, route := range UnmeteredExercisedRoutes(results) {
		workBreached[route] = true
	}
	namedMissing := make(map[string]bool)
	for _, route := range RequireNamedRoutesExercised(results, budgets) {
		namedMissing[route] = true
	}
	_, _ = fmt.Fprintf(w, "%-70s %10s %10s %13s %8s %10s %8s\n", "route", "p95", "budget", "status", "calls", "blks", "rows")
	for _, r := range results {
		if !r.Exercised {
			status := fmt.Sprintf("NOT_EXERCISED(%d)", r.Status)
			if namedMissing[r.Route] {
				// A route the latency table budgets by name must be exercised
				// (RequireNamedRoutesExercised fails the run otherwise).
				status += " BREACH"
			}
			_, _ = fmt.Fprintf(w, "%-70s %10s %10s %13s\n", r.Route, "-", "-", status)
			continue
		}
		budget := budgets.For(r.Route)
		status := "OK"
		// A HardFailed route (any 5xx sample) always breaches regardless of
		// p95 (see EvaluateBudgets) -- this status column must agree with
		// that verdict, not just compare p95 against budget, or a route the
		// gate actually fails on prints as a false "OK" here (issue #6797
		// live-gate incident: component-extensions and iac/resources showed
		// "OK" in this table while the same run's breach summary correctly
		// failed the gate on them). The same holds for the work budget: a
		// route over its Postgres work budget, or one the meter never read,
		// fails the run through EvaluateWorkBudgets and
		// UnmeteredExercisedRoutes, so it is marked here too.
		switch {
		case r.HardFailed:
			status = "BREACH"
		case workBreached[r.Route]:
			// Checked before the latency-exemption case: a route can be both
			// exempt from its ceiling and over its work budget, and the work
			// budget is this gate's actual regression-catching mechanism, so
			// that must never read as the advisory BREACH-EXEMPT below.
			status = "BREACH"
		case r.P95 > budget:
			// LatencyExemptions makes this ceiling advisory for a tracked
			// route (see EvaluateBudgets) only when nothing else about the
			// route has failed; the column still names it, so a reader never
			// mistakes BREACH-EXEMPT for a pass.
			if ex, exempt := LatencyExemptions[r.Route]; exempt {
				status = fmt.Sprintf("BREACH-EXEMPT(%s)", ex.Issue)
			} else {
				status = "BREACH"
			}
		}
		calls, blks, rows := "-", "-", "-"
		if r.Metered {
			calls = fmt.Sprintf("%.1f", r.Work.Calls)
			blks = fmt.Sprintf("%.0f", r.Work.Blks)
			rows = fmt.Sprintf("%.0f", r.Work.Rows)
		}
		_, _ = fmt.Fprintf(w, "%-70s %10s %10s %13s %8s %10s %8s\n", r.Route, r.P95.Round(time.Millisecond), budget, status, calls, blks, rows)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
