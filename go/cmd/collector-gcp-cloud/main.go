package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/redact"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// launchOptions holds the parsed command-line inputs for the scaffolding binary.
// This slice is fixture-driven: the config and redaction key are file paths so
// the binary needs no live Google Cloud access and no environment-variable
// contract (those are deferred slices).
type launchOptions struct {
	configPath       string
	redactionKeyPath string
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
	configPath := flags.String("config", "", "path to the declarative GCP collector config JSON")
	keyPath := flags.String("redaction-key-file", "", "path to the read-only redaction key material file")
	if err := flags.Parse(args); err != nil {
		return launchOptions{}, err
	}
	if strings.TrimSpace(*configPath) == "" {
		return launchOptions{}, fmt.Errorf("-config is required")
	}
	if strings.TrimSpace(*keyPath) == "" {
		return launchOptions{}, fmt.Errorf("-redaction-key-file is required")
	}
	return launchOptions{configPath: *configPath, redactionKeyPath: *keyPath}, nil
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

	redactionKey, err := loadRedactionKey(opts.redactionKeyPath)
	if err != nil {
		return err
	}

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	runner, err := buildCollectorService(
		postgres.SQLDB{DB: db},
		opts.configPath,
		redactionKey,
		tracer,
		meter,
		instruments,
		logger,
	)
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
