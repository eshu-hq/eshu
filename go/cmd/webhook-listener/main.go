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
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-webhook-listener"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		slog.Error("webhook-listener failed", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	bootstrap, err := telemetry.NewBootstrap("webhook-listener")
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

	logger := telemetry.NewLogger(bootstrap, "webhook-listener", "webhook-listener")
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}
	memLimit := runtimecfg.ConfigureMemoryLimit(logger)
	if err := telemetry.RecordGOMEMLIMIT(meter, memLimit); err != nil {
		return fmt.Errorf("register gomemlimit gauge: %w", err)
	}

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	instrumentedDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "webhook_triggers",
	}
	cfg, err := loadWebhookListenerConfig(os.Getenv)
	if err != nil {
		return err
	}
	store := postgres.NewWebhookTriggerStore(instrumentedDB)
	if err := store.EnsureSchema(parent); err != nil {
		return err
	}
	webhookMux, err := newWebhookMux(webhookHandler{
		Config:      cfg,
		Store:       store,
		Logger:      logger,
		Instruments: instruments,
		Tracer:      tracer,
	})
	if err != nil {
		return err
	}

	runtimeConfig, err := runtimecfg.LoadConfig("webhook-listener")
	if err != nil {
		return err
	}
	statusReader := postgres.NewStatusStore(postgres.SQLQueryer{DB: db})
	adminMux, err := runtimecfg.NewStatusAdminMux(
		"webhook-listener",
		statusReader,
		webhookMux,
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}
	httpServer, err := runtimecfg.NewHTTPServer(runtimecfg.HTTPServerConfig{
		Addr:    runtimeConfig.ListenAddr,
		Handler: adminMux,
	})
	if err != nil {
		return err
	}
	lifecycle, err := runtimecfg.NewLifecycle(runtimeConfig)
	if err != nil {
		return err
	}

	service := app.Application{
		Config:    runtimeConfig,
		Lifecycle: app.ComposeLifecycles(lifecycle, httpServer),
		Runner:    runtimecfg.ContextRunner{},
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return service.Run(ctx)
}
