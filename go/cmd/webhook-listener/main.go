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
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/aws"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/gcp"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/incident"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook"
	actionsstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook/actions"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
	"go.opentelemetry.io/otel/trace"
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
	runtimecfg.ConfigureMemoryLimit(logger)
	if err := telemetry.RecordGOMEMLIMIT(meter); err != nil {
		return fmt.Errorf("register gomemlimit gauge: %w", err)
	}
	cfg, err := loadWebhookListenerConfig(os.Getenv)
	if err != nil {
		return err
	}
	// Intake mode (#9377): the listener verifies and normalizes provider
	// deliveries and submits them through core's authenticated versioned
	// intake API. It opens no Postgres handle and runs no EnsureSchema:
	// core retains durable trigger storage and lifecycle.
	if intakeMode(cfg) {
		return runIntakeListener(parent, cfg, logger, tracer, instruments, providers)
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
	store := webhookstore.NewWebhookTriggerStore(instrumentedDB)
	if err := store.EnsureSchema(parent); err != nil {
		return err
	}
	actionsDB := &postgres.InstrumentedDB{Inner: postgres.SQLDB{DB: db}, Tracer: tracer, Instruments: instruments, StoreName: "cicd_actions_deliveries"}
	actionsStore := actionsstore.NewStore(actionsDB)
	if err := actionsStore.EnsureSchema(parent); err != nil {
		return err
	}
	var incidentFreshnessStore *incidentfreshnessstore.IncidentFreshnessStore
	if cfg.PagerDutySecret != "" || cfg.JiraSecret != "" {
		incidentFreshnessDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "incident_freshness_triggers",
		}
		incidentFreshnessStore = incidentfreshnessstore.NewIncidentFreshnessStore(incidentFreshnessDB)
		if err := incidentFreshnessStore.EnsureSchema(parent); err != nil {
			return err
		}
	}
	var awsFreshnessStore *awsfreshnessstore.AWSFreshnessStore
	if cfg.AWSFreshnessToken != "" {
		awsFreshnessDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "aws_freshness_triggers",
		}
		awsFreshnessStore = awsfreshnessstore.NewAWSFreshnessStore(awsFreshnessDB)
		if err := awsFreshnessStore.EnsureSchema(parent); err != nil {
			return err
		}
	}
	var gcpFreshnessStore *gcpfreshnessstore.GCPFreshnessStore
	var oidcValidator gcpPushOIDCValidator
	if gcpFreshnessConfigured(cfg) {
		gcpFreshnessDB := &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "gcp_freshness_triggers",
		}
		gcpFreshnessStore = gcpfreshnessstore.NewGCPFreshnessStore(gcpFreshnessDB)
		if err := gcpFreshnessStore.EnsureSchema(parent); err != nil {
			return err
		}
		if cfg.GCPFreshnessOIDCAudience != "" && cfg.GCPFreshnessOIDCAllowedSA != "" {
			oidcValidator = googleOIDCValidator{}
		}
	}
	// Served intake (#9404): the store-owning binary mounts core's
	// authenticated versioned intake API backed by these same Postgres
	// stores, so the satellite has a serving endpoint to submit to. A
	// trigger family without a store stays unassigned so the intake API
	// answers 503 (keep the delivery for redelivery) instead of
	// dereferencing a nil store.
	servedStores := intake.Stores{Repository: store, Actions: actionsStore}
	if incidentFreshnessStore != nil {
		servedStores.Incident = incidentFreshnessStore
	}
	if awsFreshnessStore != nil {
		servedStores.AWS = awsFreshnessStore
	}
	if gcpFreshnessStore != nil {
		servedStores.GCP = gcpFreshnessStore
	}
	servedIntake, err := buildServedIntake(cfg, servedStores, logger)
	if err != nil {
		return err
	}
	webhookMux, err := newWebhookMux(webhookHandler{
		Config:                 cfg,
		Store:                  store,
		ActionsStore:           actionsStore,
		IncidentFreshnessStore: incidentFreshnessStore,
		AWSFreshnessStore:      awsFreshnessStore,
		GCPFreshnessStore:      gcpFreshnessStore,
		GCPPushOIDCValidator:   oidcValidator,
		Logger:                 logger,
		Instruments:            instruments,
		Tracer:                 tracer,
	})
	if err != nil {
		return err
	}
	mountServedIntake(webhookMux, servedIntake)

	runtimeConfig, err := runtimecfg.LoadConfig("webhook-listener")
	if err != nil {
		return err
	}
	statusReader := postgres.NewInstrumentedStatusStore(postgres.SQLQueryer{DB: db}, instruments)
	service, err := newWebhookApplication(runtimeConfig, statusReader, webhookMux, providers.PrometheusHandler)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return service.Run(ctx)
}

// runIntakeListener runs the listener in intake mode (#9377): provider
// deliveries are verified and normalized locally, then submitted through
// core's authenticated versioned intake API. No Postgres handle is opened,
// no EnsureSchema runs, and no trigger table is touched on this path.
func runIntakeListener(
	parent context.Context,
	cfg webhookListenerConfig,
	logger *slog.Logger,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	providers *telemetry.Providers,
) error {
	client, err := newIntakeClient(cfg.IntakeURL, cfg.IntakeToken)
	if err != nil {
		return err
	}
	handler := webhookHandler{
		Config:               cfg,
		Store:                &intakeRepositorySubmitter{client: client},
		ActionsStore:         &intakeActionsSubmitter{client: client},
		Logger:               logger,
		Instruments:          instruments,
		Tracer:               tracer,
		GCPPushOIDCValidator: intakeOIDCValidatorForConfig(cfg),
	}
	if incidentWebhookConfigured(cfg) {
		handler.IncidentFreshnessStore = &intakeIncidentSubmitter{client: client}
	}
	if cfg.AWSFreshnessToken != "" {
		handler.AWSFreshnessStore = &intakeAWSFreshnessSubmitter{client: client}
	}
	if gcpFreshnessConfigured(cfg) {
		handler.GCPFreshnessStore = &intakeGCPFreshnessSubmitter{client: client}
	}
	webhookMux, err := newWebhookMux(handler)
	if err != nil {
		return err
	}
	runtimeConfig, err := runtimecfg.LoadConfig("webhook-listener")
	if err != nil {
		return err
	}
	statusReader := &intakeStatusReader{baseURL: strings.TrimRight(strings.TrimSpace(cfg.IntakeURL), "/")}
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
