// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	// Blank import populates the AWS scanner registry so AWS freshness
	// webhook handlers can validate incoming service_kind values against the
	// scanner set the collector ships.
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
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
	var incidentFreshnessStore *postgres.IncidentFreshnessStore
	if cfg.PagerDutySecret != "" || cfg.JiraSecret != "" {
		incidentFreshnessDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "incident_freshness_triggers",
		}
		incidentFreshnessStore = postgres.NewIncidentFreshnessStore(incidentFreshnessDB)
		if err := incidentFreshnessStore.EnsureSchema(parent); err != nil {
			return err
		}
	}
	var awsFreshnessStore *postgres.AWSFreshnessStore
	if cfg.AWSFreshnessToken != "" {
		awsFreshnessDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "aws_freshness_triggers",
		}
		awsFreshnessStore = postgres.NewAWSFreshnessStore(awsFreshnessDB)
		if err := awsFreshnessStore.EnsureSchema(parent); err != nil {
			return err
		}
	}
	var gcpFreshnessStore *postgres.GCPFreshnessStore
	if cfg.GCPFreshnessToken != "" {
		gcpFreshnessDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "gcp_freshness_triggers",
		}
		gcpFreshnessStore = postgres.NewGCPFreshnessStore(gcpFreshnessDB)
		if err := gcpFreshnessStore.EnsureSchema(parent); err != nil {
			return err
		}
	}
	webhookMux, err := newWebhookMux(webhookHandler{
		Config:                 cfg,
		Store:                  store,
		IncidentFreshnessStore: incidentFreshnessStore,
		AWSFreshnessStore:      awsFreshnessStore,
		GCPFreshnessStore:      gcpFreshnessStore,
		Logger:                 logger,
		Instruments:            instruments,
		Tracer:                 tracer,
	})
	if err != nil {
		return err
	}

	runtimeConfig, err := runtimecfg.LoadConfig("webhook-listener")
	if err != nil {
		return err
	}
	statusReader := postgres.NewStatusStore(postgres.SQLQueryer{DB: db})
	service, err := newWebhookApplication(runtimeConfig, statusReader, webhookMux, providers.PrometheusHandler)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return service.Run(ctx)
}

func newWebhookApplication(
	runtimeConfig runtimecfg.Config,
	statusReader statuspkg.Reader,
	webhookMux http.Handler,
	prometheusHandler http.Handler,
) (app.Application, error) {
	adminMux, err := runtimecfg.NewStatusAdminMux(
		runtimeConfig.ServiceName,
		statusReader,
		webhookMux,
		runtimecfg.WithPrometheusHandler(prometheusHandler),
	)
	if err != nil {
		return app.Application{}, err
	}
	httpServer, err := runtimecfg.NewHTTPServer(runtimecfg.HTTPServerConfig{
		Addr:    runtimeConfig.ListenAddr,
		Handler: adminMux,
	})
	if err != nil {
		return app.Application{}, err
	}
	lifecycle, err := runtimecfg.NewLifecycle(runtimeConfig)
	if err != nil {
		return app.Application{}, err
	}
	combined := app.ComposeLifecycles(lifecycle, httpServer)

	metricsAddr := strings.TrimSpace(runtimeConfig.MetricsAddr)
	if metricsAddr != "" && metricsAddr != strings.TrimSpace(runtimeConfig.ListenAddr) {
		metricsServer, err := runtimecfg.NewStatusMetricsServer(
			runtimeConfig,
			statusReader,
			runtimecfg.WithPrometheusHandler(prometheusHandler),
		)
		if err != nil {
			return app.Application{}, err
		}
		if metricsServer != nil {
			combined = app.ComposeLifecycles(combined, metricsServer)
		}
	}

	return app.Application{
		Config:    runtimeConfig,
		Lifecycle: combined,
		Runner:    runtimecfg.ContextRunner{},
	}, nil
}
