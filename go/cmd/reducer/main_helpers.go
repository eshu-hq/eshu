// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/clock"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

// configureReducerQueue builds the reducer work queue and applies the retry,
// claim-domain, projector-drain, and semantic-claim tuning loaded from the
// environment, logging the source-local-projector and semantic-claim-limit
// settings when they are active. Extracted from buildReducerService to keep
// main.go within the repo file-size budget.
func configureReducerQueue(
	database postgres.ExecQueryer,
	retryCfg runtimecfg.RetryPolicyConfig,
	claimDomains []reducer.Domain,
	projectorDrainGate bool,
	getenv func(string) string,
	graphBackend runtimecfg.GraphBackend,
	clk clock.Clock,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) postgres.ReducerQueue {
	workQueue := postgres.NewReducerQueue(database, "reducer", time.Minute)
	// Injected clock for lease TTL / claim visibility / retry timing (#4121);
	// clock.System().Now() == time.Now() in production, swappable for replay.
	workQueue.Now = clk.Now
	workQueue.RetryDelay = retryCfg.RetryDelay
	workQueue.MaxAttempts = retryCfg.MaxAttempts
	// Exponential backoff + jitter (#4450): without these, same-instant
	// failures reconverge on one visible_at and self-reinforce into a retry
	// storm. See runtime.RetryPolicyConfig's doc comment for the formula.
	workQueue.MaxRetryDelay = retryCfg.MaxRetryDelay
	workQueue.JitterFraction = retryCfg.JitterFraction
	workQueue.Instruments = instruments
	workQueue.ClaimDomains = claimDomains
	workQueue.RequireProjectorDrainBeforeClaim = projectorDrainGate
	workQueue.ExpectedSourceLocalProjectors = loadReducerExpectedSourceLocalProjectors(getenv)
	workQueue.SemanticEntityClaimLimit = loadReducerSemanticEntityClaimLimit(getenv, graphBackend)
	if workQueue.ExpectedSourceLocalProjectors > 0 && logger != nil {
		logger.Info(
			"semantic reducers will wait for expected source-local projectors",
			"expected_source_local_projectors", workQueue.ExpectedSourceLocalProjectors,
		)
	}
	if projectorDrainGate && logger != nil {
		logger.Info(
			"semantic reducer claim limit configured",
			"semantic_entity_claim_limit", workQueue.SemanticEntityClaimLimit,
		)
	}
	return workQueue
}

// configureGraphProjectionRepairQueue builds the readiness repair queue with the
// injected clock so its bookkeeping timestamps share the queue/lease time source
// (#4121). Extracted from buildReducerService to keep the entrypoint within the
// repo file-size budget.
func configureGraphProjectionRepairQueue(
	database postgres.ExecQueryer,
	clk clock.Clock,
) *postgres.GraphProjectionPhaseRepairQueueStore {
	queue := postgres.NewGraphProjectionPhaseRepairQueueStore(database)
	queue.Now = clk.Now
	return queue
}

// graphProjectionPhaseRepairerFor builds the readiness repair runner with the
// injected clock (#4121) so its due-time selection and retry backoff use the
// same time source as the reducer queue. Extracted from buildReducerService to
// keep the entrypoint within the repo file-size budget.
func graphProjectionPhaseRepairerFor(
	queue reducer.GraphProjectionPhaseRepairQueue,
	database postgres.ExecQueryer,
	stateStore *postgres.GraphProjectionPhaseStateStore,
	config reducer.GraphProjectionPhaseRepairerConfig,
	clk clock.Clock,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) *reducer.GraphProjectionPhaseRepairer {
	return &reducer.GraphProjectionPhaseRepairer{
		Queue:       queue,
		AcceptedGen: postgres.NewAcceptedGenerationLookup(database),
		StateLookup: stateStore,
		Publisher:   stateStore,
		Config:      config,
		Now:         clk.Now,
		Instruments: instruments,
		Logger:      logger,
	}
}

// reducerGraphDrainFor returns a ReducerGraphDrain when the projector drain gate
// is enabled, otherwise nil. Extracted from buildReducerService to keep main.go
// within the repo file-size budget.
func reducerGraphDrainFor(enabled bool, queryer postgres.Queryer) reducer.ReducerGraphDrain {
	if !enabled {
		return nil
	}
	return postgres.NewReducerGraphDrain(queryer)
}

// registerReducerObservableGauges wires the reducer's OpenTelemetry observable
// gauges: queue depth/oldest-age (eshu_dp_queue_depth,
// eshu_dp_queue_oldest_age_seconds), the active worker-pool gauge
// (eshu_dp_worker_pool_active, backed by activeWorkers), and the
// shared-acceptance read-model gauge (eshu_dp_shared_acceptance_rows). The queue
// and acceptance observers read cheap, bounded queries; the worker observer
// reads an in-memory atomic counter. The graph orphan observer runs static-label
// capped counts. The provenance observers (eshu_dp_edges_by_source_tool,
// eshu_dp_files_by_language) run bounded LIMIT-capped aggregation queries
// through the graph read port. None add unbounded scan cost per metrics scrape.
// It lives here rather than in main.go to keep that file within the file-size
// budget.
func registerReducerObservableGauges(
	instruments *telemetry.Instruments,
	meter metric.Meter,
	db *sql.DB,
	activeWorkers *atomic.Int64,
	graphOrphanObserver telemetry.GraphOrphanObserver,
	graphReader query.GraphQuery,
	getenv func(string) string,
) error {
	queueObserver := postgres.NewQueueObserverStore(postgres.SQLQueryer{DB: db})
	queueObserver.Now = clock.System().Now // explicit seam (#4121); == time.Now()
	workerObserver := reducerWorkerObserver{active: activeWorkers}
	if err := telemetry.RegisterObservableGauges(instruments, meter, queueObserver, workerObserver); err != nil {
		return fmt.Errorf("register observable gauges: %w", err)
	}

	acceptanceObserver := postgres.NewSharedProjectionAcceptanceStore(postgres.SQLDB{DB: db})
	if err := telemetry.RegisterAcceptanceObservableGauges(instruments, meter, acceptanceObserver); err != nil {
		return fmt.Errorf("register acceptance observable gauge: %w", err)
	}
	if err := telemetry.RegisterGraphOrphanObservableGauge(instruments, meter, graphOrphanObserver); err != nil {
		return fmt.Errorf("register graph orphan observable gauge: %w", err)
	}

	workflowFamilyQueueObserver := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: db})
	if err := telemetry.RegisterWorkflowFamilyQueueDepthObservableGauge(instruments, meter, workflowFamilyQueueObserver); err != nil {
		return fmt.Errorf("register workflow family queue depth observable gauge: %w", err)
	}

	activeGenerationObserver := activeGenerationAgeObserverFor(postgres.SQLDB{DB: db}, loadGenerationLivenessConfig(getenv))
	if err := telemetry.RegisterActiveGenerationAgeObservableGauge(instruments, meter, activeGenerationObserver); err != nil {
		return fmt.Errorf("register active generation age observable gauge: %w", err)
	}

	// The poison stuck-gauge is wired unconditionally (unlike the recovery
	// runner) so the dead-letter/poison class is always visible to an operator
	// regardless of whether bounded auto-retry is enabled (#4740).
	poisonObserver := poisonLivenessObserverFor(postgres.SQLDB{DB: db})
	if err := telemetry.RegisterPoisonLivenessObservableGauges(instruments, meter, poisonObserver); err != nil {
		return fmt.Errorf("register poison liveness observable gauges: %w", err)
	}

	if err := registerProvenanceCoverageGauges(instruments, meter, graphReader, getenv); err != nil {
		return err
	}
	return nil
}

func graphOrphanObserver(service reducer.Service) telemetry.GraphOrphanObserver {
	if service.GraphOrphanSweepRunner == nil || service.GraphOrphanSweepRunner.Sweeper == nil {
		return nil
	}
	observer, _ := service.GraphOrphanSweepRunner.Sweeper.(telemetry.GraphOrphanObserver)
	return observer
}

// main is the reducer entrypoint. It prints the version when requested, then
// runs the service loop. It lives in this helper file rather than main.go to
// keep that file within the repo file-size budget.
func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-reducer"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		slog.Error("reducer failed", "error", err)
		os.Exit(1)
	}
}

// reducerDomainStrings renders reducer domains as plain strings for structured
// log fields. It lives here rather than in main.go to keep that file within the
// repo file-size budget.
func reducerDomainStrings(domains []reducer.Domain) []string {
	values := make([]string, 0, len(domains))
	for _, domain := range domains {
		values = append(values, string(domain))
	}
	return values
}

// incidentRepositoryCorrelationWiring builds the production adapters for the
// durable incident -> repository correlation domain (#2161). The applied-routing
// loader supplies the PagerDuty provider service id and the Terraform backend
// locator; the backend resolver maps that locator to a single owning config
// repository using the same tfstatebackend join the config/state and cloud
// runtime drift domains use, so every backend-ownership consumer agrees. Only
// confident single-owner resolutions emit a durable edge; weaker signals stay
// provenance-only and fail-closed. It is extracted from the entrypoint so the
// reducer command stays within the repo file-size budget.
func incidentRepositoryCorrelationWiring(database postgres.ExecQueryer) (
	reducer.AppliedPagerDutyServiceRoutingLoader,
	reducer.BackendRepositoryResolver,
	reducer.IncidentRepositoryCorrelationWriter,
) {
	loader := postgres.PostgresAppliedPagerDutyServiceRoutingLoader{DB: database}
	resolver := postgres.BackendRepositoryResolverAdapter{
		Resolver: tfstatebackend.NewResolver(
			postgres.PostgresTerraformBackendQuery{DB: database},
		),
	}
	writer := reducer.PostgresIncidentRepositoryCorrelationWriter{DB: database}
	return loader, resolver, writer
}

// codeReachabilityProjectionRunnerFor wires the reachability read-model runner.
// It reuses the backend-aware reducer worker count (ESHU_REDUCER_WORKERS) as the
// disjoint-partition fan-out so operators tune both with one knob; the runner
// clamps the value to the host CPU count.
func codeReachabilityProjectionRunnerFor(
	database postgres.ExecQueryer,
	sharedCfg reducer.SharedProjectionRunnerConfig,
	concurrency int,
	logger *slog.Logger,
) *reducer.CodeReachabilityProjectionRunner {
	store := postgres.NewCodeReachabilityStore(database)
	return &reducer.CodeReachabilityProjectionRunner{
		InputLoader: store,
		RowWriter:   store,
		Config: reducer.CodeReachabilityProjectionRunnerConfig{
			PollInterval: sharedCfg.PollInterval,
			BatchLimit:   sharedCfg.BatchLimit,
			Concurrency:  concurrency,
		},
		Logger: logger,
	}
}

// searchVectorSeedTimeout bounds the synchronous seeder call at startup so a
// slow Postgres connection or large corpus never blocks the reducer
// indefinitely before the build runner starts.
const searchVectorSeedTimeout = 5 * time.Minute

// seedSearchVectorScopeState seeds the #4233 versioned scope-state tables
// exactly once at reducer startup, after schema apply. It gates on the same
// condition that wires the runner (nil runner = vectors disabled). The caller
// provides the startup context; this function wraps it with a bounded timeout.
func seedSearchVectorScopeState(
	seedCtx context.Context,
	runner *reducer.SearchVectorBuildRunner,
	database postgres.ExecQueryer,
	logger *slog.Logger,
) error {
	if runner == nil {
		return nil
	}
	identity := postgres.EshuSearchVectorIdentity{
		ProviderProfileID:  runner.Config.ProviderProfileID,
		SourceClass:        runner.Config.SourceClass,
		EmbeddingModelID:   runner.Config.EmbeddingModelID,
		VectorIndexVersion: runner.Config.VectorIndexVersion,
	}
	if logger != nil {
		logger.Info("seeding search vector scope state",
			"provider_profile_id", identity.ProviderProfileID,
			"embedding_model_id", identity.EmbeddingModelID,
		)
	}
	seedStart := time.Now()
	seedCtx, seedCancel := context.WithTimeout(seedCtx, searchVectorSeedTimeout)
	defer seedCancel()
	if err := postgres.SeedSearchVectorScopeState(seedCtx, database, identity); err != nil {
		return fmt.Errorf("seed search vector scope state: %w", err)
	}
	if logger != nil {
		logger.Info("search vector scope state seeded",
			"duration", time.Since(seedStart).String(),
		)
	}
	return nil
}
