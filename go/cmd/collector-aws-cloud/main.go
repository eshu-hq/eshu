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
	// Blank import installs the full AWS scanner registry via init side
	// effects. Adding a new scanner means appending one underscore-import to
	// the bindings package, with no change in this command.
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
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
	mode       launchMode
	configPath string
}

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-collector-aws-cloud"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap("collector-aws-cloud")
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("collector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, "collector-aws-cloud", "collector-aws-cloud")

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		logger.Error("collector-aws-cloud argument parsing failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}

	if err := run(context.Background(), opts); err != nil {
		logger.Error("collector-aws-cloud failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

// parseArgs parses the collector mode and the fixture config path. The default
// mode is claimed-live so existing live deployments keep their behavior; the
// fixture mode is opt-in and requires a config file.
func parseArgs(args []string) (launchOptions, error) {
	flags := flag.NewFlagSet("collector-aws-cloud", flag.ContinueOnError)
	mode := flags.String("mode", string(launchModeClaimedLive), "collector mode: fixture or claimed-live")
	configPath := flags.String("config", "", "path to the declarative AWS collector fixture config JSON (fixture mode only)")
	if err := flags.Parse(args); err != nil {
		return launchOptions{}, err
	}
	selectedMode := launchMode(strings.TrimSpace(*mode))
	if selectedMode == "" {
		selectedMode = launchModeClaimedLive
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
	default:
		return launchOptions{}, fmt.Errorf("unsupported -mode %q", selectedMode)
	}
	return launchOptions{mode: selectedMode, configPath: strings.TrimSpace(*configPath)}, nil
}

func run(parent context.Context, opts launchOptions) error {
	bootstrap, err := telemetry.NewBootstrap("collector-aws-cloud")
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

	logger := telemetry.NewLogger(bootstrap, "collector-aws-cloud", "collector-aws-cloud")
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

	runner, err := buildRuntimeRunner(db, opts, tracer, meter, instruments, logger)
	if err != nil {
		return err
	}
	service, err := app.NewHostedWithStatusServer(
		"collector-aws-cloud",
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

// buildRuntimeRunner selects the offline fixture service or the live
// workflow-claimed service for the requested mode.
func buildRuntimeRunner(
	db *sql.DB,
	opts launchOptions,
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
			tracer,
			instruments,
			logger,
		)
	case launchModeClaimedLive:
		storeDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "collector_aws_cloud",
		}
		return buildClaimedService(storeDB, os.Getenv, tracer, instruments, logger, meter)
	default:
		return nil, fmt.Errorf("unsupported mode %q", opts.mode)
	}
}
