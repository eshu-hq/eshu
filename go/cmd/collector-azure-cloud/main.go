// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/redact"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type launchMode string

const (
	launchModeFixture     launchMode = "fixture"
	launchModeClaimedLive launchMode = "claimed-live"
)

// launchOptions holds the parsed command-line inputs for the collector binary.
type launchOptions struct {
	mode             launchMode
	redactionKeyPath string
}

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-collector-azure-cloud"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap("collector-azure-cloud")
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("collector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, "collector-azure-cloud", "collector-azure-cloud")

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		logger.Error("collector-azure-cloud argument parsing failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}

	if err := run(context.Background(), opts); err != nil {
		logger.Error("collector-azure-cloud failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

// parseArgs parses the launch mode and redaction-key file path. Fixture mode
// keeps reading its declarative targets and optional redaction key from the
// environment (backward compatible). Claimed-live mode requires the read-only
// redaction key file so live tag observation never runs unkeyed.
func parseArgs(args []string) (launchOptions, error) {
	flags := flag.NewFlagSet("collector-azure-cloud", flag.ContinueOnError)
	mode := flags.String("mode", string(launchModeFixture), "collector mode: fixture or claimed-live")
	keyPath := flags.String("redaction-key-file", "", "path to the read-only redaction key material file (required in claimed-live mode)")
	if err := flags.Parse(args); err != nil {
		return launchOptions{}, err
	}
	selectedMode := launchMode(strings.TrimSpace(*mode))
	if selectedMode == "" {
		selectedMode = launchModeFixture
	}
	switch selectedMode {
	case launchModeFixture:
	case launchModeClaimedLive:
		if strings.TrimSpace(*keyPath) == "" {
			return launchOptions{}, fmt.Errorf("-redaction-key-file is required in claimed-live mode")
		}
	default:
		return launchOptions{}, fmt.Errorf("unsupported -mode %q", selectedMode)
	}
	return launchOptions{mode: selectedMode, redactionKeyPath: strings.TrimSpace(*keyPath)}, nil
}

func run(parent context.Context, opts launchOptions) error {
	bootstrap, err := telemetry.NewBootstrap("collector-azure-cloud")
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

	logger := telemetry.NewLogger(bootstrap, "collector-azure-cloud", "collector-azure-cloud")
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
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

	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	runner, err := buildRuntimeRunner(parent, db, opts, tracer, meter, instruments, logger)
	if err != nil {
		return err
	}
	service, err := app.NewHostedWithStatusServer(
		"collector-azure-cloud",
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

// buildRuntimeRunner selects the fixture or claimed-live runner. Fixture mode
// preserves the environment-driven offline source; claimed-live mode wires the
// workflow-claimed live runtime.
func buildRuntimeRunner(
	ctx context.Context,
	db *sql.DB,
	opts launchOptions,
	tracer trace.Tracer,
	meter metric.Meter,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (app.Runner, error) {
	storeDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "collector_azure_cloud",
	}
	switch opts.mode {
	case launchModeFixture:
		return buildCollectorService(storeDB, os.Getenv, tracer, meter, logger)
	case launchModeClaimedLive:
		redactionKey, err := loadRequiredRedactionKey(opts.redactionKeyPath)
		if err != nil {
			return nil, err
		}
		return buildClaimedService(ctx, storeDB, redactionKey, os.Getenv, tracer, meter, instruments, logger)
	default:
		return nil, fmt.Errorf("unsupported mode %q", opts.mode)
	}
}

// loadRequiredRedactionKey reads the read-only redaction key material from path
// and rejects a blank file so the claimed-live runtime never emits facts with
// an unkeyed marker. The material is never logged.
func loadRequiredRedactionKey(path string) (redact.Key, error) {
	key, err := loadRedactionKey(path)
	if err != nil {
		return redact.Key{}, err
	}
	if key.IsZero() {
		return redact.Key{}, fmt.Errorf("claimed-live mode requires a non-empty redaction key file")
	}
	return key, nil
}
