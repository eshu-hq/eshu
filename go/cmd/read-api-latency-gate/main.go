// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/reducer/contract"
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
	mcpBaseURL := flag.String("mcp-base-url", "", "running MCP HTTP transport base URL for pilot tools/call")
	apiKey := flag.String("api-key", os.Getenv("ESHU_API_KEY"), "eshu-api bearer token")
	totalScopes := flag.Int("total-scopes", 800, "total ingestion_scopes rows to seed")
	nodesPerLabel := flag.Int("nodes-per-label", 150000, "synthetic graph nodes to seed per infra label")
	iacFactCount := flag.Int("iac-fact-count", 150000, "IaC content_entity fact_records rows to seed (issue #6793: currentInventoryCTE jsonb detoast cost)")
	winnersFactCount := flag.Int("winners-fact-count", 20000, "reducer_supply_chain_impact_finding fact_records rows to seed for the winners-read leg (issue #11170: rebuilt into supply_chain_impact_canonical_winners so findings serve byte-identical from the read model)")
	sharedIntentCount := flag.Int("shared-intent-count", defaultSharedIntentCount, "shared_projection_intents rows to seed, newest 1% pending (issue #6820: the status routes' domain backlog aggregate must read pending intents by index, not scan the table)")
	iterations := flag.Int("iterations", 20, "requests per route for the p95 sweep")
	runs := flag.Int("runs", 1, "counted sweeps per route after discarded warmups; later passes have no additional warmup (default 1: P95 uses the first counted pass)")
	budgetsPath := flag.String("budgets", "testdata/benchmarks/read-api-route-budgets.txt", "route latency budget table path")
	workBudgetsPath := flag.String("work-budgets", "testdata/benchmarks/read-api-route-work-budgets.txt", "route Postgres work budget table path")
	workReportPath := flag.String("work-report", "", "write the per-route Postgres work measured this run as JSON (input to scripts/refresh-read-api-work-budgets.sh)")
	latencyReportPath := flag.String("latency-report", "", "write the full per-route latency distribution (first/later samples, identity block) as JSON, before budget evaluation")
	eshuCommit := flag.String("eshu-commit", os.Getenv("GATE_ESHU_COMMIT"), "eshu git commit to record in the latency report identity block (best-effort)")
	apiBinarySHA256 := flag.String("api-binary-sha256", os.Getenv("GATE_API_BINARY_SHA256"), "eshu-api binary sha256 to record in the latency report identity block (best-effort)")
	coreBinarySHA256 := flag.String("core-binary-sha256", os.Getenv("GATE_CORE_BINARY_SHA256"), "eshu-core-api binary sha256 to record in the latency report identity block (best-effort)")
	winnersRead := flag.Bool("winners-read", false, "mark this sweep as the winners-read leg in the latency report identity block (the core serves findings from the maintained winners table)")
	backgroundIdle := flag.Duration("background-idle", 5*time.Second, "idle window used to measure background Postgres statements before the sweep")
	skipSeed := flag.Bool("skip-seed", false, "skip Postgres+graph seeding and sweep an already-seeded database")
	seedOnly := flag.Bool("seed-only", false, "seed Postgres+graph, verify the counts, and exit without sweeping (the core and satellite start after this; the seed already ran the infra read model backfill, see seed_infra_backfill.go)")
	requestTimeout := flag.Duration("request-timeout", 30*time.Second, "per-request timeout for the sweep")
	concurrentWorkers := flag.Int("concurrent-workers", 0, "optional bounded pilot read concurrency after the metered sequential stage (2..16)")
	concurrentRequests := flag.Int("concurrent-requests", 20, "requests per pilot operation in the optional concurrent stage")
	concurrentReport := flag.String("concurrent-report", "", "optional JSON report for the concurrent pilot stage")
	diagnosticReport := flag.String("diagnostic-client-report", "", "optional bounded status client correlation report; requires concurrent pilot and API diagnostic mode")
	flag.Parse()

	if err := run(runOptions{
		postgresDSN:        *postgresDSN,
		graphURI:           *graphURI,
		graphDatabase:      *graphDatabase,
		graphUsername:      *graphUsername,
		graphPassword:      *graphPassword,
		apiBaseURL:         *apiBaseURL,
		mcpBaseURL:         *mcpBaseURL,
		apiKey:             *apiKey,
		totalScopes:        *totalScopes,
		nodesPerLabel:      *nodesPerLabel,
		iacFactCount:       *iacFactCount,
		winnersFactCount:   *winnersFactCount,
		sharedIntents:      *sharedIntentCount,
		iterations:         *iterations,
		runs:               *runs,
		budgetsPath:        *budgetsPath,
		workBudgetsPath:    *workBudgetsPath,
		workReportPath:     *workReportPath,
		latencyReportPath:  *latencyReportPath,
		eshuCommit:         *eshuCommit,
		apiBinarySHA256:    *apiBinarySHA256,
		coreBinarySHA256:   *coreBinarySHA256,
		winnersRead:        *winnersRead,
		backgroundIdle:     *backgroundIdle,
		skipSeed:           *skipSeed,
		seedOnly:           *seedOnly,
		requestTimeout:     *requestTimeout,
		concurrentWorkers:  *concurrentWorkers,
		concurrentRequests: *concurrentRequests,
		concurrentReport:   *concurrentReport,
		diagnosticReport:   *diagnosticReport,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "read-api-latency-gate:", err)
		os.Exit(1)
	}
}

type runOptions struct {
	postgresDSN        string
	graphURI           string
	graphDatabase      string
	graphUsername      string
	graphPassword      string
	apiBaseURL         string
	mcpBaseURL         string
	apiKey             string
	totalScopes        int
	nodesPerLabel      int
	iacFactCount       int
	winnersFactCount   int
	sharedIntents      int
	iterations         int
	runs               int
	budgetsPath        string
	workBudgetsPath    string
	workReportPath     string
	latencyReportPath  string
	eshuCommit         string
	apiBinarySHA256    string
	coreBinarySHA256   string
	winnersRead        bool
	backgroundIdle     time.Duration
	skipSeed           bool
	seedOnly           bool
	requestTimeout     time.Duration
	concurrentWorkers  int
	concurrentRequests int
	concurrentReport   string
	diagnosticReport   string
	diagnostics        *diagnosticClient
	statementCapture   diagnosticStatementCapture
}

func run(opts runOptions) (runErr error) {
	ctx := context.Background()

	if err := validateRunOptions(opts); err != nil {
		return err
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
	if opts.diagnosticReport != "" {
		opts.diagnostics = newDiagnosticClient(4096)
		defer func() {
			runErr = errors.Join(runErr, writeDiagnosticClientReport(opts.diagnosticReport, opts.diagnostics, opts.concurrentRequests))
		}()
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
	routes, operations, err := selectedOperations(inventory, opts.mcpBaseURL != "")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: sweeping %d read operations x %d iterations\n", len(routes), opts.iterations)

	results, err := SweepRoutes(SweepOptions{
		BaseURL:     opts.apiBaseURL,
		MCPBaseURL:  opts.mcpBaseURL,
		Operations:  operations,
		APIKey:      opts.apiKey,
		Routes:      routes,
		QueryArgs:   RouteQueryArgs,
		Iterations:  opts.iterations,
		Runs:        opts.runs,
		Timeout:     opts.requestTimeout,
		Meter:       meter,
		Context:     ctx,
		Diagnostics: opts.diagnostics,
	})
	if err != nil {
		return fmt.Errorf("sweep routes: %w", err)
	}
	if err := writeWorkReportFile(opts.workReportPath, results); err != nil {
		return err
	}
	// Written before budget evaluation below: a leg that goes on to breach
	// its budget must still yield a report -- the report is measurement,
	// not a verdict, and a comparator needs both legs' reports regardless of
	// which one (if either) is green.
	if err := writeLatencyReportFile(opts.latencyReportPath, opts, results); err != nil {
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

	exercised, coverageFloor := printCoverage(os.Stderr, results, operations)

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

	if coverageErr := CheckCoverageFloor(results, coverageFloor); coverageErr != nil {
		fmt.Fprintf(os.Stderr, "read-api-latency-gate: %v\n", coverageErr)
		failures = append(failures, coverageErr.Error())
	}

	if missing := RequireNamedRoutesExercised(results, budgets); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "read-api-latency-gate: %d explicitly-budgeted route(s) were not exercised: %s\n", len(missing), strings.Join(missing, ", "))
		failures = append(failures, fmt.Sprintf("%d explicitly-budgeted route(s) were not exercised", len(missing)))
	}
	failures = append(failures, pilotFailures(results, operations)...)

	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	if err := runConcurrentPilot(opts, operations, budgets); err != nil {
		return err
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

	intents, err := BuildSharedIntentPlan(plan, SharedIntentPlanOptions{
		Total:   opts.sharedIntents,
		Pending: sharedIntentPending(opts.sharedIntents, defaultSharedIntentPendingPercent),
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d shared projection intents across %d domains (newest %d pending; the corpus the status routes' domain backlog aggregate reads)\n",
		intents.Len(), len(contract.ProjectionDomains()), sharedIntentPending(opts.sharedIntents, defaultSharedIntentPendingPercent))
	if err := SeedSharedIntents(ctx, pool, intents); err != nil {
		return err
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

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d cloud/state facts per label on scope %s (the fact corpus /cloud/inventory reads)\n", opts.nodesPerLabel, iacScope.ScopeID)
	cloudStateFacts := BuildCloudStateFacts(iacScope.ScopeID, iacScope.ActiveGenerationID, opts.nodesPerLabel)
	if err := SeedCloudStateFacts(ctx, pool, cloudStateFacts, time.Now().UTC()); err != nil {
		return fmt.Errorf("seed cloud/state facts: %w", err)
	}

	// The winners-read leg (issue #11170) needs live finding facts AND the
	// reconciled read model: seed reducer_supply_chain_impact_finding facts
	// across every scope's active generation, then rebuild the winners table
	// through the maintainer's own full resweep so the table cannot drift
	// from what production would derive. A bulk COPY of facts the
	// active-facts join cannot match (wrong generation, tombstoned) would
	// pass this count check while leaving winners empty, so the rebuild
	// counts and the non-empty-table proof below are the real validity
	// gate for the leg.
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d supply-chain impact finding facts for the winners-read leg\n", opts.winnersFactCount)
	winnersFacts := BuildWinnersFindingFacts(winnersAnchorsFromPlan(plan), opts.winnersFactCount, winnersSourcesPerGroup)
	if err := SeedWinnersFactRecords(ctx, pool, winnersFacts, time.Now().UTC()); err != nil {
		return fmt.Errorf("seed winners finding facts: %w", err)
	}

	expectedCounts := expectedRelationalCounts(plan, iacFacts)
	expectedCounts["fact_records"] += len(cloudStateFacts) + len(winnersFacts)
	addSharedIntentCounts(expectedCounts, intents)
	if err := VerifyRelationalCounts(ctx, pool, expectedCounts); err != nil {
		return fmt.Errorf("verify seeded Postgres tables: %w", err)
	}

	if inserted, updated, deleted, unchanged, err := rebuildSeededWinners(ctx, opts.postgresDSN, expectedWinnerGroups(opts.winnersFactCount, winnersSourcesPerGroup)); err != nil {
		return fmt.Errorf("rebuild seeded winners: %w", err)
	} else {
		fmt.Fprintf(os.Stderr, "read-api-latency-gate: winners rebuild inserted=%d updated=%d deleted=%d unchanged=%d\n",
			inserted, updated, deleted, unchanged)
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

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding story plus search content_files and gate content_entities (#7009 pilots)\n")
	if err := SeedContentFiles(ctx, pool, time.Now().UTC()); err != nil {
		return fmt.Errorf("seed story and search content: %w", err)
	}
	if err := VerifyContentFileCounts(ctx, pool, ExpectedContentFileCounts()); err != nil {
		return fmt.Errorf("verify seeded content: %w", err)
	}
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: finalizing deferred content search indexes after the bulk content drain\n")
	if err := finalizeContentSearchIndexes(ctx, opts.postgresDSN); err != nil {
		return fmt.Errorf("seed content search index finalization: %w", err)
	}
	if err := VerifyContentSearchReadiness(ctx, pool); err != nil {
		return fmt.Errorf("verify content search readiness: %w", err)
	}

	fmt.Fprintf(os.Stderr, "read-api-latency-gate: seeding %d story repository nodes and edges\n", SeedStoryRepositoryCount)
	if err := SeedRepositoryGraph(ctx, graphOpts); err != nil {
		return fmt.Errorf("seed story repository graph: %w", err)
	}
	if err := VerifyRepositoryGraphCounts(ctx, graphOpts); err != nil {
		return fmt.Errorf("verify story repository graph: %w", err)
	}

	// The infra read model derives from the content_entities seeded above and is
	// ready only once its backfill marker exists. The gate starts no reducer
	// (which owns the backfill since #11210), so the seed runs the same
	// Backfiller before any core starts; dropping this stand-in is #11226.
	if err := backfillSeededInfraReadModel(ctx, opts.postgresDSN, pool, os.Stderr); err != nil {
		return fmt.Errorf("backfill seeded infra read model: %w", err)
	}

	fmt.Fprintln(os.Stderr, "read-api-latency-gate: ANALYZE seeded Postgres tables")
	if err := analyzeSeededTables(ctx, pool); err != nil {
		return fmt.Errorf("analyze seeded tables: %w", err)
	}

	return nil
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func validateRunOptions(opts runOptions) error {
	if err := ValidateLatencyExemptions(LatencyExemptions); err != nil {
		return fmt.Errorf("latency exemptions: %w", err)
	}
	if opts.postgresDSN == "" {
		return fmt.Errorf("postgres-dsn (or ESHU_POSTGRES_DSN) is required to seed and to meter Postgres work")
	}
	if opts.seedOnly && opts.skipSeed {
		return fmt.Errorf("-seed-only and -skip-seed are mutually exclusive")
	}
	if !opts.skipSeed && opts.winnersFactCount <= 0 {
		return fmt.Errorf("-winners-fact-count must be positive when seeding (a zero count would leave the winners leg sweeping an empty read model)")
	}
	if err := validateOperationOptions(opts); err != nil {
		return err
	}
	return nil
}
