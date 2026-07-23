// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/graphschemacompat"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-projector"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap("projector")
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("projector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, "projector", "projector")

	if err := run(context.Background()); err != nil {
		logger.Error("projector failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	// Initialize telemetry
	bootstrap, err := telemetry.NewBootstrap("projector")
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

	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}
	logger := telemetry.NewLogger(bootstrap, "projector", "projector")

	memLimit := runtimecfg.ConfigureMemoryLimit(logger)
	if err := telemetry.RecordGOMEMLIMIT(meter, memLimit); err != nil {
		return fmt.Errorf("register gomemlimit gauge: %w", err)
	}
	logger.Info("starting projector")

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
	if _, err := graphschemacompat.RequireCompatibleForRuntime(parent, postgres.SQLQueryer{DB: db}, os.Getenv); err != nil {
		return err
	}

	canonicalWriter, canonicalCloser, err := openProjectorCanonicalWriter(parent, postgres.SQLDB{DB: db}, os.Getenv, tracer, instruments)
	if err != nil {
		return err
	}
	defer func() {
		_ = canonicalCloser.Close()
	}()

	runner, err := buildProjectorService(postgres.SQLDB{DB: db}, canonicalWriter, os.Getenv, tracer, instruments, logger)
	if err != nil {
		return err
	}
	retryPolicy, err := loadProjectorRetryPolicy(os.Getenv)
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
	service, err := app.NewHostedWithStatusServer(
		"projector",
		runner,
		statusReader,
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	// stop is called exactly once, inside runServiceAndJoinRedrive below --
	// deliberately NOT also deferred here. No fallible step runs between this
	// line and that call, so there is no early-return path that would leak
	// the signal-relay goroutine signal.NotifyContext starts.

	// Crossplane cross-scope SATISFIED_BY redrive catch-up (issue #5476
	// P1-a): recovers a sweep left mid-fan-out by a transient DB error or a
	// crashed process. Runs alongside the main projector Service.Run loop,
	// sharing its shutdown context, so it stops together with the service.
	//
	// Joined via runServiceAndJoinRedrive, NOT via a deferred redriveWG.Wait()
	// after service.Run: that ordering deadlocks the whole process on a
	// fatal (non-signal) service error -- see runServiceAndJoinRedrive's doc
	// comment (issue #5476 P0) for why stop() must be called unconditionally
	// before the join rather than relied upon via deferred LIFO ordering.
	var redriveWG sync.WaitGroup
	redriveReducerQueue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "projector", time.Minute)
	crossplaneRedriveSweeper := buildCrossplaneRedriveSweeper(postgres.SQLDB{DB: db}, redriveReducerQueue, instruments)
	redriveWG.Add(1)
	go func() {
		defer redriveWG.Done()
		runCrossplaneRedriveCatchUpLoop(ctx, crossplaneRedriveSweeper, logger)
	}()

	return runServiceAndJoinRedrive(ctx, service, stop, &redriveWG)
}
