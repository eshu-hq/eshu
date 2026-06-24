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
	logger *slog.Logger,
) postgres.ReducerQueue {
	workQueue := postgres.NewReducerQueue(database, "reducer", time.Minute)
	workQueue.RetryDelay = retryCfg.RetryDelay
	workQueue.MaxAttempts = retryCfg.MaxAttempts
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
// capped counts. None add unbounded scan cost per metrics scrape. It lives here
// rather than in main.go to keep that file within the file-size budget.
func registerReducerObservableGauges(
	instruments *telemetry.Instruments,
	meter metric.Meter,
	db *sql.DB,
	activeWorkers *atomic.Int64,
	graphOrphanObserver telemetry.GraphOrphanObserver,
	getenv func(string) string,
) error {
	queueObserver := postgres.NewQueueObserverStore(postgres.SQLQueryer{DB: db})
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
