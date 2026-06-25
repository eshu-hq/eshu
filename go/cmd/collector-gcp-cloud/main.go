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
	"path/filepath"
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
	launchModeCassette    launchMode = "cassette"
)

// launchOptions holds the parsed command-line inputs for the scaffolding binary.
type launchOptions struct {
	mode             launchMode
	configPath       string
	redactionKeyPath string
	cassetteFile     string
}

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-collector-gcp-cloud"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap("collector-gcp-cloud")
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("collector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, "collector-gcp-cloud", "collector-gcp-cloud")

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		logger.Error("collector-gcp-cloud argument parsing failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}

	if err := run(context.Background(), opts); err != nil {
		logger.Error("collector-gcp-cloud failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

// parseArgs parses the config and redaction-key file paths.
func parseArgs(args []string) (launchOptions, error) {
	flags := flag.NewFlagSet("collector-gcp-cloud", flag.ContinueOnError)
	mode := flags.String("mode", string(launchModeFixture), "collector mode: fixture, claimed-live, or cassette")
	configPath := flags.String("config", "", "path to the declarative GCP collector config JSON")
	keyPath := flags.String("redaction-key-file", "", "path to the read-only redaction key material file")
	cassetteFile := flags.String("cassette-file", "", "path to a cassette JSON file (cassette mode only)")
	if err := flags.Parse(args); err != nil {
		return launchOptions{}, err
	}
	selectedMode := launchMode(strings.TrimSpace(*mode))
	if selectedMode == "" {
		selectedMode = launchModeFixture
	}
	switch selectedMode {
	case launchModeFixture:
		if strings.TrimSpace(*configPath) == "" {
			return launchOptions{}, fmt.Errorf("-config is required in fixture mode")
		}
	case launchModeClaimedLive:
		if strings.TrimSpace(*configPath) != "" {
			return launchOptions{}, fmt.Errorf("-config is not used in claimed-live mode")
		}
	case launchModeCassette:
		if strings.TrimSpace(*cassetteFile) == "" {
			return launchOptions{}, fmt.Errorf("-cassette-file is required in cassette mode")
		}
	default:
		return launchOptions{}, fmt.Errorf("unsupported -mode %q", selectedMode)
	}
	if selectedMode != launchModeCassette && strings.TrimSpace(*keyPath) == "" {
		return launchOptions{}, fmt.Errorf("-redaction-key-file is required")
	}
	return launchOptions{
		mode:             selectedMode,
		configPath:       strings.TrimSpace(*configPath),
		redactionKeyPath: *keyPath,
		cassetteFile:     strings.TrimSpace(*cassetteFile),
	}, nil
}

func run(parent context.Context, opts launchOptions) error {
	bootstrap, err := telemetry.NewBootstrap("collector-gcp-cloud")
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

	logger := telemetry.NewLogger(bootstrap, "collector-gcp-cloud", "collector-gcp-cloud")
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

	var redactionKey redact.Key
	if opts.mode != launchModeCassette {
		redactionKey, err = loadRedactionKey(opts.redactionKeyPath)
		if err != nil {
			return err
		}
	}

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	runner, err := buildRuntimeRunner(parent, db, opts, redactionKey, tracer, meter, instruments, logger)
	if err != nil {
		return err
	}
	service, err := app.NewHostedWithStatusServer(
		"collector-gcp-cloud",
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

func buildRuntimeRunner(
	ctx context.Context,
	db *sql.DB,
	opts launchOptions,
	redactionKey redact.Key,
	tracer trace.Tracer,
	meter metric.Meter,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (app.Runner, error) {
	switch opts.mode {
	case launchModeFixture:
		return buildCollectorService(
			postgres.SQLDB{DB: db},
			opts.configPath,
			redactionKey,
			tracer,
			meter,
			instruments,
			logger,
		)
	case launchModeClaimedLive:
		storeDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "collector_gcp_cloud",
		}
		return buildClaimedService(ctx, storeDB, redactionKey, os.Getenv, tracer, meter, instruments, logger)
	case launchModeCassette:
		storeDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "collector_gcp_cloud",
		}
		return buildCassetteService(storeDB, opts.cassetteFile, tracer, instruments, logger)
	default:
		return nil, fmt.Errorf("unsupported mode %q", opts.mode)
	}
}

// loadRedactionKey reads the read-only redaction key material from a file. The
// material is never logged. A blank file is rejected so facts are never emitted
// with an unkeyed marker.
func loadRedactionKey(path string) (redact.Key, error) {
	material, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return redact.Key{}, fmt.Errorf("read gcp redaction key file: %w", err)
	}
	key, err := redact.NewKey(material)
	if err != nil {
		return redact.Key{}, fmt.Errorf("gcp redaction key: %w", err)
	}
	return key, nil
}
