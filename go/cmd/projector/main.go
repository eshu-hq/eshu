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

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	canonicalWriter, canonicalCloser, err := openProjectorCanonicalWriter(parent, os.Getenv, tracer, instruments)
	if err != nil {
		return err
	}
	defer func() {
		_ = canonicalCloser.Close()
	}()

	runner, err := buildProjectorService(postgres.SQLDB{DB: db}, canonicalWriter, os.Getenv)
	if err != nil {
		return err
	}
	retryPolicy, err := loadProjectorRetryPolicy(os.Getenv)
	if err != nil {
		return err
	}
	statusReader := statuspkg.WithRetryPolicies(
		postgres.NewStatusStore(postgres.SQLQueryer{DB: db}),
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
	defer stop()

	return service.Run(ctx)
}
