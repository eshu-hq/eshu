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
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-collector-git"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap("collector-git")
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("collector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, "collector-git", "collector-git")

	if err := run(context.Background()); err != nil {
		logger.Error("collector-git failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	bootstrap, err := telemetry.NewBootstrap("collector-git")
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

	logger := telemetry.NewLogger(bootstrap, "collector-git", "collector-git")
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

	runner, err := buildCollectorService(
		postgres.SQLDB{DB: db},
		os.Getenv,
		tracer,
		instruments,
		logger,
	)
	if err != nil {
		return err
	}
	service, err := app.NewHostedWithStatusServer(
		"collector-git",
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
