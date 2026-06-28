// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/replay/recorder"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const serviceName = "collector-kubernetes-live"

type launchMode string

const (
	launchModeCassette launchMode = "cassette"
	launchModeLive     launchMode = "live"
	launchModeRecord   launchMode = "record"
)

// launchOptions holds the parsed command-line inputs for the collector binary.
type launchOptions struct {
	mode         launchMode
	cassetteFile string
}

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-collector-kubernetes-live"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap(serviceName)
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("collector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, serviceName, serviceName)

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		logger.Error(serviceName+" argument parsing failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}

	if err := run(context.Background(), opts); err != nil {
		logger.Error(serviceName+" failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

// parseArgs parses the collector launch mode. The default mode is live.
// Cassette mode replays a -cassette-file credential-free; record mode runs the
// live collector and writes a canonical cassette to -cassette-file.
func parseArgs(args []string) (launchOptions, error) {
	flags := flag.NewFlagSet(serviceName, flag.ContinueOnError)
	mode := flags.String("mode", string(launchModeLive), "collector mode: live, cassette, or record")
	cassetteFile := flags.String("cassette-file", "", "cassette JSON path (replayed in cassette mode, written in record mode)")
	if err := flags.Parse(args); err != nil {
		return launchOptions{}, err
	}
	selectedMode := launchMode(strings.TrimSpace(*mode))
	if selectedMode == "" {
		selectedMode = launchModeLive
	}
	switch selectedMode {
	case launchModeLive:
	case launchModeCassette, launchModeRecord:
		if strings.TrimSpace(*cassetteFile) == "" {
			return launchOptions{}, fmt.Errorf("-cassette-file is required in %s mode", selectedMode)
		}
	default:
		return launchOptions{}, fmt.Errorf("unsupported -mode %q", selectedMode)
	}
	return launchOptions{mode: selectedMode, cassetteFile: strings.TrimSpace(*cassetteFile)}, nil
}

func run(parent context.Context, opts launchOptions) error {
	bootstrap, err := telemetry.NewBootstrap(serviceName)
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

	logger := telemetry.NewLogger(bootstrap, serviceName, serviceName)
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	// Record mode is a one-shot credentialed fixture run: it drives the live
	// source and writes a canonical cassette, with no durable commit and no
	// status server, so it needs the collector's credentials but no database.
	if opts.mode == launchModeRecord {
		return runRecord(parent, opts.cassetteFile, tracer, instruments, logger)
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

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	storeDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "collector_kubernetes_live",
	}

	var runner app.Runner
	switch opts.mode {
	case launchModeCassette:
		runner, err = buildCassetteService(storeDB, opts.cassetteFile, tracer, instruments, logger)
	default:
		runner, err = buildCollectorService(storeDB, os.Getenv, tracer, instruments, logger)
	}
	if err != nil {
		return err
	}

	service, err := app.NewHostedWithStatusServer(
		serviceName,
		runner,
		postgres.NewStatusStore(postgres.SQLQueryer{DB: db}),
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return service.Run(ctx)
}

// runRecord drives the live Kubernetes source for one batch and writes a
// canonical cassette to cassettePath. The cassette object_id values are the
// collector's real facts.StableID derivation because the real source runs here,
// which is the structural fix for cassette object_id fidelity (#3928).
func runRecord(
	parent context.Context,
	cassettePath string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	config, err := loadRuntimeConfig(os.Getenv)
	if err != nil {
		return err
	}
	source := newLiveSource(config, tracer, instruments, logger)

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("recording cassette", "event_name", "collector.record.started", "path", cassettePath)
	if err := recorder.Run(ctx, source, recorder.Options{
		Path:           cassettePath,
		CollectorLabel: "kubernetes_live",
	}); err != nil {
		return fmt.Errorf("record cassette: %w", err)
	}
	logger.Info("recorded cassette", "event_name", "collector.record.completed", "path", cassettePath)
	return nil
}
