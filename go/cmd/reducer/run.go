// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/graphschemacompat"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/census"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func run(parent context.Context) error {
	// Initialize telemetry
	bootstrap, err := telemetry.NewBootstrap("reducer")
	if err != nil {
		return fmt.Errorf("telemetry bootstrap: %w", err)
	}
	providers, err := telemetry.NewProviders(parent, bootstrap)
	if err != nil {
		return fmt.Errorf("telemetry providers: %w", err)
	}
	defer func() {
		_ = providers.Shutdown(context.Background())
	}()

	logger := telemetry.NewLogger(bootstrap, "reducer", "reducer")
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	logger.Info("starting reducer")

	pprofSrv, err := runtimecfg.NewPprofServer(os.Getenv)
	if err != nil {
		return fmt.Errorf("pprof server: %w", err)
	}
	if pprofSrv != nil {
		if err := pprofSrv.Start(parent); err != nil {
			return fmt.Errorf("pprof server start: %w", err)
		}
		logger.Info("pprof server listening", "addr", pprofSrv.Addr())
		defer func() {
			_ = pprofSrv.Stop(context.Background())
		}()
	}

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	// The fence in migration 109 marks content writes from connections
	// without the derive-aware writer setting; say loudly if ours lack it.
	inventory.VerifyWriterSession(parent, postgres.SQLQueryer{DB: db}, logger)
	if _, err := graphschemacompat.RequireCompatibleForRuntime(parent, postgres.SQLQueryer{DB: db}, os.Getenv); err != nil {
		return err
	}

	neo4jExecutor, cypherExecutor, neo4jReader, graphReader, neo4jCloser, err := openReducerNeo4jAdapters(parent, os.Getenv, instruments)
	if err != nil {
		return err
	}
	defer func() { _ = neo4jCloser.Close() }()

	serviceRunner, graphRefresher, postgresRefresher, err := buildObservedReducerService(parent, db, neo4jExecutor, cypherExecutor, neo4jReader, graphReader, os.Getenv, tracer, instruments, meter, logger)
	if err != nil {
		return err
	}
	retryPolicy, err := loadReducerQueueConfig(os.Getenv)
	if err != nil {
		return err
	}
	statusReader := statuspkg.WithRetryPolicies(
		postgres.NewInstrumentedStatusStore(postgres.SQLQueryer{DB: db}, instruments),
		statuspkg.MergeRetryPolicies(
			statuspkg.DefaultRetryPolicies(),
			statuspkg.RetryPolicySummary{
				Stage:       "reducer",
				MaxAttempts: retryPolicy.MaxAttempts,
				RetryDelay:  retryPolicy.RetryDelay,
			},
		)...,
	)
	service, err := app.NewHostedWithStatusServer(
		"reducer",
		serviceRunner,
		statusReader,
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}

	graphBackend, err := runtimecfg.LoadGraphBackend(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	// Start the gauge refreshers under the shutdown context and stop them
	// before the deferred graph-driver and database closes run, so a shutdown
	// never fails an in-flight refresh read against a closed backend (#7062,
	// #7064). stop runs first so the waits cannot block on a still-live
	// context when service.Run returns an error.
	waitGraphRefresher := startGraphGaugeRefresher(ctx, graphRefresher)
	waitPostgresRefresher := startPostgresGaugeRefresher(ctx, postgresRefresher)
	waitPackageManifestBackfill := startPackageManifestConsumptionKeyBackfill(ctx, func(runCtx context.Context) error {
		return runPackageManifestConsumptionKeyBackfill(runCtx, db, instruments, logger)
	}, logger)
	// The id-anchor census (#7212) reads the graph, so it stops before the
	// deferred graph-driver close like the gauge refreshers above.
	waitIDAnchorCensus := startIDAnchorCensus(ctx, loadIDAnchorCensusConfig(os.Getenv), graphBackend, idAnchorCensusReader(neo4jReader, graphReader), instruments, logger)
	defer func() {
		stop()
		waitIDAnchorCensus()
		waitPackageManifestBackfill()
		waitGraphRefresher()
		waitPostgresRefresher()
	}()

	startSearchDocumentSweeper(ctx, db, logger)
	startConfigStateDriftCatchUpSweeper(ctx, db, instruments, logger)

	return service.Run(ctx)
}

// startIDAnchorCensus starts the id-anchor census loop (#7212) when it should
// run and returns a wait function for shutdown before the graph driver closes.
// When it does not run it logs why once and the returned function is a no-op.
func startIDAnchorCensus(
	ctx context.Context,
	cfg idAnchorCensusConfig,
	backend runtimecfg.GraphBackend,
	reader anchor.RowReader,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) func() {
	if !idAnchorCensusShouldRun(cfg, backend) || reader == nil {
		logger.Info("id anchor census not started",
			slog.Bool("enabled", cfg.Enabled),
			slog.String("graph_backend", string(backend)),
			slog.Bool("reader_configured", reader != nil))
		return func() {}
	}
	runner := &census.Runner{
		Source:      anchor.ReaderCensus{Reader: reader},
		Instruments: instruments,
		Logger:      logger,
		Interval:    cfg.PollInterval,
		Timeout:     cfg.Timeout,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("id anchor census stopped", slog.String("error", err.Error()))
		}
	}()
	return func() { <-done }
}

// idAnchorCensusReader picks the graph read port for the census. It prefers the
// raw session runner over the differential-capture decorator: the census is an
// operator probe, not a production query, and a recorded copy of it would be a
// Neo4j-only statement in every golden-corpus capture and show up as a backend
// divergence. It falls back to the decorated reader when the raw port is not a
// single-row reader, and returns nil when neither is configured.
func idAnchorCensusReader(raw sourcecypher.CypherReader, decorated query.GraphQuery) anchor.RowReader {
	if rowReader, ok := raw.(anchor.RowReader); ok {
		return rowReader
	}
	if decorated != nil {
		return decorated
	}
	return nil
}
