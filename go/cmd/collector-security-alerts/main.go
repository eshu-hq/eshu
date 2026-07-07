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

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const runtimeName = "collector-security-alerts"

type launchMode string

const (
	launchModeCassette    launchMode = "cassette"
	launchModeClaimedLive launchMode = "claimed-live"
)

// launchOptions holds the parsed collector launch inputs.
type launchOptions struct {
	mode         launchMode
	cassetteFile string
}

// parseArgs parses the collector launch mode. The default is claimed-live;
// cassette mode replays a -cassette-file credential-free for the golden-corpus
// gate.
func parseArgs(args []string) (launchOptions, error) {
	flags := flag.NewFlagSet(runtimeName, flag.ContinueOnError)
	mode := flags.String("mode", string(launchModeClaimedLive), "collector mode: claimed-live or cassette")
	cassetteFile := flags.String("cassette-file", "", "path to a cassette JSON file (cassette mode only)")
	if err := flags.Parse(args); err != nil {
		return launchOptions{}, err
	}
	selectedMode := launchMode(strings.TrimSpace(*mode))
	if selectedMode == "" {
		selectedMode = launchModeClaimedLive
	}
	switch selectedMode {
	case launchModeClaimedLive:
	case launchModeCassette:
		if strings.TrimSpace(*cassetteFile) == "" {
			return launchOptions{}, fmt.Errorf("-cassette-file is required in cassette mode")
		}
	default:
		return launchOptions{}, fmt.Errorf("unsupported -mode %q", selectedMode)
	}
	return launchOptions{mode: selectedMode, cassetteFile: strings.TrimSpace(*cassetteFile)}, nil
}

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-collector-security-alerts"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if hasProviderAccessPreflightFlag(os.Args[1:]) {
		if err := runProviderAccessPreflight(context.Background(), os.Getenv); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_, _ = fmt.Fprintln(os.Stdout, "security alert provider access preflight passed")
		return
	}

	bootstrap, err := telemetry.NewBootstrap("collector-security-alerts")
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("collector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, "collector-security-alerts", "collector-security-alerts")

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		logger.Error(runtimeName+" argument parsing failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}

	if err := run(context.Background(), opts); err != nil {
		logger.Error("collector-security-alerts failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context, opts launchOptions) error {
	bootstrap, err := telemetry.NewBootstrap("collector-security-alerts")
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

	logger := telemetry.NewLogger(bootstrap, "collector-security-alerts", "collector-security-alerts")
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
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
		StoreName:   "collector_security_alerts",
	}
	var runner app.Runner
	switch opts.mode {
	case launchModeCassette:
		runner, err = buildCassetteService(storeDB, opts.cassetteFile, tracer, instruments, logger)
	default:
		runner, err = buildClaimedService(storeDB, os.Getenv, tracer, instruments, logger)
	}
	if err != nil {
		return err
	}
	service, err := app.NewHostedWithStatusServer(
		"collector-security-alerts",
		runner,
		postgres.NewInstrumentedStatusStore(postgres.SQLQueryer{DB: db}, instruments),
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return service.Run(ctx)
}
