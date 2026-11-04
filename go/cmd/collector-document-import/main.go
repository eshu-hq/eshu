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

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const runtimeName = "collector-document-import"

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-collector-document-import"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap(runtimeName)
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("collector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, runtimeName, runtimeName)

	if err := run(context.Background(), logger); err != nil {
		logger.Error("collector-document-import failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context, logger *slog.Logger) error {
	bootstrap, err := telemetry.NewBootstrap(runtimeName)
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

	cfg, err := loadImportConfig(os.Getenv)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		logger.Info("document import collector disabled",
			"event_name", "collector.disabled",
			"detail", "set "+envEnabled+"=true with an artifact dir and pinned engine to enable",
		)
		return nil
	}

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

	db, err := runtimecfg.OpenPostgresForWorkload(parent, os.Getenv, runtimecfg.PostgresWorkloadRuntime)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	storeDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "collector_document_import",
	}
	drain, err := buildDrain(storeDB, cfg, tracer, instruments, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	generations, envelopes, err := drain(ctx)
	if err != nil {
		return err
	}
	logger.Info("document import drain complete",
		"event_name", "collector.drain.complete",
		"generations", generations,
		"envelopes", envelopes,
	)
	return nil
}
