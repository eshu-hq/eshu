// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/graphschemacompat"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/census"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/legacy/retire"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	auditstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
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
	graphBackend, err := runtimecfg.LoadGraphBackend(os.Getenv)
	if err != nil {
		return err
	}
	// POST /admin/retire-legacy-workloads (#8795): operator-driven retirement
	// of pre-#5385 name-only Workload nodes, served on the admin listener.
	legacyRetirement, err := legacyWorkloadRetirementHandler(cypherExecutor, neo4jReader, graphReader, graphBackend, db, instruments, logger)
	if err != nil {
		return err
	}
	service, err := app.NewHostedWithStatusServer(
		"reducer",
		serviceRunner,
		statusReader,
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
		runtimecfg.WithLegacyWorkloadRetirement(legacyRetirement),
	)
	if err != nil {
		return err
	}

	repairConfig, err := loadSecretLinesRepairConfig(os.Getenv)
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
	// The secret-lines repair (#8833) rebuilds the hardcoded-secret side table
	// whenever its state is not ready. Its lock session comes from a dedicated
	// pool, never the shared one, and both stop before the database closes.
	secretLinesLockPool, err := openSecretLinesLockPool(parent, repairConfig, os.Getenv)
	if err != nil {
		return err
	}
	waitSecretLinesRepair := startSecretLinesRepair(ctx, repairConfig,
		lines.NewRepairer(postgres.SQLDB{DB: db}, secretLinesLockPool, lines.RepairOptions{
			PollInterval: repairConfig.PollInterval, Logger: logger, Instruments: instruments,
		}), logger)
	defer func() {
		stop()
		waitSecretLinesRepair()
		closeSecretLinesLockPool(secretLinesLockPool)
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
func idAnchorCensusReader(raw node.CypherReader, decorated query.GraphQuery) anchor.RowReader {
	if rowReader, ok := raw.(anchor.RowReader); ok {
		return rowReader
	}
	if decorated != nil {
		return decorated
	}
	return nil
}

// legacyWorkloadRetirementHandler builds the POST /admin/retire-legacy-workloads
// handler for the reducer admin listener (#8795).
//
// Reads go through the raw session runner when it can serve rows: like the
// id-anchor census this is an operator probe, and a recorded copy of it would
// appear as a graph-specific statement in a differential capture. Dry runs
// and execute are served only on Neo4j, the one backend the detection and the
// retirement statement were tested on; on any other backend the route answers
// 501 to both.
func legacyWorkloadRetirementHandler(
	cypherExec reducer.CypherExecutor,
	rawReader node.CypherReader,
	decorated query.GraphQuery,
	backend runtimecfg.GraphBackend,
	db *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*runtimecfg.LegacyWorkloadRetirementHandler, error) {
	reader, _ := rawReader.(legacyworkload.Reader)
	if reader == nil && decorated != nil {
		reader = decorated
	}
	writer, _ := cypherExec.(retire.Writer)
	var audit retire.Auditor
	if db != nil {
		audit = auditstore.NewGovernanceAuditStore(postgres.SQLDB{DB: db})
	}
	runner := &retire.Runner{
		Reader:           reader,
		Writer:           writer,
		BackendValidated: backend == runtimecfg.GraphBackendNeo4j,
		Audit:            audit,
		Instruments:      instruments,
		Logger:           logger,
	}
	return runtimecfg.NewLegacyWorkloadRetirementHandler(runner)
}

// secretLinesRepairConfig is the reducer's hardcoded-secret side table repair
// configuration (#8833 ruling-3).
type secretLinesRepairConfig struct {
	// Enabled starts the repair loop. It is on by default; off is for
	// diagnostics only, and reads then stay on the legacy scan whenever the
	// state is not ready.
	Enabled bool
	// PollInterval is the wait between polls of the state row.
	PollInterval time.Duration
}

// loadSecretLinesRepairConfig reads ESHU_SECRET_LINES_REPAIR_ENABLED (default
// true) and ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL (default 30s, at least 1s).
// A value that does not parse stops startup with an error naming the variable.
func loadSecretLinesRepairConfig(getenv func(string) string) (secretLinesRepairConfig, error) {
	config := secretLinesRepairConfig{Enabled: true, PollInterval: 30 * time.Second}
	if raw := getenv("ESHU_SECRET_LINES_REPAIR_ENABLED"); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return secretLinesRepairConfig{}, fmt.Errorf("ESHU_SECRET_LINES_REPAIR_ENABLED=%q: want true or false", raw)
		}
		config.Enabled = enabled
	}
	if raw := getenv("ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL"); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil || interval < time.Second {
			return secretLinesRepairConfig{}, fmt.Errorf("ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL=%q: want a duration of at least 1s", raw)
		}
		config.PollInterval = interval
	}
	return config, nil
}

// secretLinesRepairRunner is the loop startSecretLinesRepair runs;
// *lines.Repairer satisfies it.
type secretLinesRepairRunner interface {
	Run(context.Context) error
}

// startSecretLinesRepair starts the repair loop under ctx when enabled and
// returns a wait function for shutdown. When disabled it logs once and the
// returned function is a no-op.
func startSecretLinesRepair(ctx context.Context, config secretLinesRepairConfig, runner secretLinesRepairRunner, logger *slog.Logger) func() {
	if !config.Enabled {
		logger.Warn("secret lines repair disabled; hardcoded-secret reads stay on the legacy scan whenever the side table is not ready",
			telemetry.EventAttr("secret_lines.repair_disabled"))
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("secret lines repair stopped", telemetry.EventAttr("secret_lines.repair_stopped"), "error", err.Error())
		}
	}()
	return func() { <-done }
}

// openSecretLinesLockPool opens the secret-lines Repairer's lock pool (#8833
// ruling-4): the runtime's Postgres opener with the reducer's DSN, lifetime, and
// application name, then one connection and none kept idle. The Repairer pins a
// connection from it for a whole rebuild while Finalize needs the shared pool's
// capacity, so taking the lock from the shared pool would starve Finalize when
// ESHU_POSTGRES_MAX_OPEN_CONNS is 1 (the pool-of-one deadlock). The cost is
// that the reducer opens up to ESHU_POSTGRES_MAX_OPEN_CONNS + 1 connections
// while a rebuild runs; the extra one exists only for that time. It returns nil
// when the repair is disabled.
func openSecretLinesLockPool(ctx context.Context, config secretLinesRepairConfig, getenv func(string) string) (*sql.DB, error) {
	if !config.Enabled {
		return nil, nil
	}
	pool, err := runtimecfg.OpenPostgres(ctx, getenv)
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	return pool, nil
}

// closeSecretLinesLockPool closes the lock pool opened by
// openSecretLinesLockPool; a nil pool (repair disabled) is a no-op.
func closeSecretLinesLockPool(pool *sql.DB) {
	if pool != nil {
		_ = pool.Close()
	}
}
