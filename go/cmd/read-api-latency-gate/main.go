// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
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
	budgetsPath := flag.String("budgets", "testdata/benchmarks/read-api-route-budgets.txt", "route budget table path")
	skipSeed := flag.Bool("skip-seed", false, "skip Postgres+graph seeding and sweep an already-seeded database")
	requestTimeout := flag.Duration("request-timeout", 30*time.Second, "per-request timeout for the sweep")
	flag.Parse()

	if err := run(runOptions{
		postgresDSN:    *postgresDSN,
		graphURI:       *graphURI,
		graphDatabase:  *graphDatabase,
		graphUsername:  *graphUsername,
		graphPassword:  *graphPassword,
		apiBaseURL:     *apiBaseURL,
		apiKey:         *apiKey,
		totalScopes:    *totalScopes,
		nodesPerLabel:  *nodesPerLabel,
		iacFactCount:   *iacFactCount,
		iterations:     *iterations,
		budgetsPath:    *budgetsPath,
		skipSeed:       *skipSeed,
		requestTimeout: *requestTimeout,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "read-api-latency-gate:", err)
		os.Exit(1)
	}
}

type runOptions struct {
	postgresDSN    string
	graphURI       string
	graphDatabase  string
	graphUsername  string
	graphPassword  string
	apiBaseURL     string
	apiKey         string
	totalScopes    int
	nodesPerLabel  int
	iacFactCount   int
	iterations     int
	budgetsPath    string
	skipSeed       bool
	requestTimeout time.Duration
}

func run(opts runOptions) error {
	ctx := context.Background()

	if !opts.skipSeed {
		if opts.postgresDSN == "" {
			return fmt.Errorf("postgres-dsn (or ESHU_POSTGRES_DSN) is required to seed")
		}
		if err := seed(ctx, opts); err != nil {
			return err
		}
	}

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
	})
	if err != nil {
		return fmt.Errorf("sweep routes: %w", err)
	}

	budgetsFile, err := os.Open(opts.budgetsPath)
	if err != nil {
		return fmt.Errorf("open budgets file %s: %w", opts.budgetsPath, err)
	}
	defer func() { _ = budgetsFile.Close() }()
	budgets, err := ParseRouteBudgets(budgetsFile)
	if err != nil {
		return fmt.Errorf("parse budgets file %s: %w", opts.budgetsPath, err)
	}
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		return fmt.Errorf("load capability catalog: %w", err)
	}
	budgets = budgets.WithCatalog(catalog)

	printReport(results, budgets)

	exercised, total := ExercisedCoverage(results)
	fmt.Fprintf(os.Stderr, "\nread-api-latency-gate: exercised %d/%d routes (floor %d)\n", exercised, total, ExercisedCoverageFloor)

	var failures []string

	if breaches := EvaluateBudgets(results, budgets); len(breaches) > 0 {
		fmt.Fprintf(os.Stderr, "\nread-api-latency-gate: %d route(s) exceeded budget:\n", len(breaches))
		for _, b := range breaches {
			fmt.Fprintf(os.Stderr, "  %s: p95 %s > budget %s\n", b.Route, b.P95, b.Budget)
		}
		failures = append(failures, fmt.Sprintf("%d route(s) exceeded their latency budget", len(breaches)))
	}

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

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d nodes per infra label (%d labels)\n", opts.nodesPerLabel, len(infraLabels))
	if err := SeedGraph(ctx, SeedGraphOptions{
		URI:           opts.graphURI,
		Username:      opts.graphUsername,
		Password:      opts.graphPassword,
		DatabaseName:  opts.graphDatabase,
		NodesPerLabel: opts.nodesPerLabel,
	}); err != nil {
		return fmt.Errorf("seed graph: %w", err)
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
	_, err := pool.Exec(ctx, "ANALYZE ingestion_scopes, scope_generations, fact_work_items, fact_records")
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

func printReport(results []RouteLatency, budgets RouteBudgets) {
	fmt.Printf("%-70s %10s %10s %13s\n", "route", "p95", "budget", "status")
	for _, r := range results {
		if !r.Exercised {
			fmt.Printf("%-70s %10s %10s %13s\n", r.Route, "-", "-", fmt.Sprintf("NOT_EXERCISED(%d)", r.Status))
			continue
		}
		budget := budgets.For(r.Route)
		status := "OK"
		if r.P95 > budget {
			status = "BREACH"
		}
		fmt.Printf("%-70s %10s %10s %13s\n", r.Route, r.P95.Round(time.Millisecond), budget, status)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
