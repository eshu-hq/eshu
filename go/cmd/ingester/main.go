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

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/graphschemacompat"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-ingester"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		slog.Error("ingester failed", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	// Initialize telemetry
	bootstrap, err := telemetry.NewBootstrap("ingester")
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

	logger := telemetry.NewLogger(bootstrap, "collector", "ingester")
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	memLimit := runtimecfg.ConfigureMemoryLimit(logger)
	if err := telemetry.RecordGOMEMLIMIT(meter, memLimit); err != nil {
		return fmt.Errorf("register gomemlimit gauge: %w", err)
	}
	// Reap adopted git-helper zombies: every fetch/clone orphans one
	// short-lived helper grandchild that reparents to PID 1, and without an
	// init process those zombies accumulate until fork fails pod-wide.
	runtimecfg.StartOrphanReaper(parent, logger)
	logger.Info("starting ingester")

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
	defer func() {
		_ = db.Close()
	}()
	// The fence in migration 109 marks content writes from connections
	// without the derive-aware writer setting; say loudly if ours lack it.
	inventory.VerifyWriterSession(parent, postgres.SQLQueryer{DB: db}, logger)
	if _, err := graphschemacompat.RequireCompatibleForRuntime(parent, postgres.SQLQueryer{DB: db}, os.Getenv); err != nil {
		return err
	}

	// The queue gauges never query Postgres on a scrape: a background
	// refresher runs the bounded queue reads on its own goroutines and the
	// gauge callbacks serve the last snapshot (#7064).
	queueObserver := postgres.NewQueueObserverStore(postgres.SQLQueryer{DB: db})
	postgresRefresher, err := registerPostgresQueueGauges(instruments, meter, queueObserver, os.Getenv, logger)
	if err != nil {
		return fmt.Errorf("register postgres queue gauges: %w", err)
	}

	canonicalWriter, canonicalCloser, err := openIngesterCanonicalWriter(parent, postgres.SQLDB{DB: db}, os.Getenv, logger, tracer, instruments)
	if err != nil {
		return err
	}
	defer func() {
		_ = canonicalCloser.Close()
	}()

	instrumentedDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "ingester",
	}

	runner, err := buildIngesterService(
		instrumentedDB,
		canonicalWriter,
		os.Getenv,
		os.Getwd,
		os.Environ,
		tracer,
		instruments,
		logger,
	)
	if err != nil {
		return err
	}

	retryPolicy, err := loadIngesterRetryPolicy(os.Getenv)
	if err != nil {
		return err
	}
	statusReader := statuspkg.WithRetryPolicies(
		postgres.NewInstrumentedStatusStore(postgres.SQLQueryer{DB: db}, instruments),
		statuspkg.MergeRetryPolicies(
			statuspkg.DefaultRetryPolicies(),
			statuspkg.RetryPolicySummary{
				Stage:       "projector",
				MaxAttempts: retryPolicy.MaxAttempts,
				RetryDelay:  retryPolicy.RetryDelay,
			},
		)...,
	)

	recoveryStore := postgres.NewRecoveryStore(postgres.SQLDB{DB: db})
	recoveryHandler, err := recovery.NewHandler(recoveryStore)
	if err != nil {
		return err
	}
	httpRecovery, err := runtimecfg.NewRecoveryHandler(recoveryHandler)
	if err != nil {
		return err
	}

	service, err := app.NewHostedWithStatusServer(
		"ingester", runner, statusReader,
		runtimecfg.WithRecoveryHandler(httpRecovery),
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)

	// Start the Postgres gauge refresher under the shutdown context and stop
	// it before the deferred database close runs (LIFO), so a shutdown never
	// fails an in-flight refresh read against a closed pool (#7064). stop
	// runs first so the wait cannot block on a still-live context.
	waitPostgresGauges := startPostgresQueueGauges(ctx, postgresRefresher)
	defer func() {
		stop()
		waitPostgresGauges()
	}()

	return service.Run(ctx)
}
