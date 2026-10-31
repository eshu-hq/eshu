// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
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
	// Blank import populates the AWS scanner registry so the served intake
	// API validates incoming service_kind values against the same scanner
	// set as the webhook listener's served intake path.
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	awsfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/aws"
	gcpfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/gcp"
	incidentfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/incident"
	webhookstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook"
	actionsstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook/actions"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-webhook-intake"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		slog.Error("webhook-intake failed", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	bootstrap, err := telemetry.NewBootstrap("webhook-intake")
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

	logger := telemetry.NewLogger(bootstrap, "webhook-intake", "webhook-intake")
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}
	runtimecfg.ConfigureMemoryLimit(logger)
	cfg, err := loadWebhookIntakeConfig(os.Getenv)
	if err != nil {
		return err
	}
	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// The intake host is the canonical owner of EnsureSchema for the five
	// trigger tables (#8876). It serves every intake family unconditionally:
	// unlike the listener, store presence is not gated on provider tokens
	// because this host never faces providers — the satellite presents the
	// Bearer and submits any family. A missing store would answer 503
	// (keep for redelivery), so all five are always wired.
	stores, err := openTriggerStores(parent, db, tracer, instruments)
	if err != nil {
		return err
	}
	intakeServer, err := buildIntakeServer(cfg, stores, logger)
	if err != nil {
		return err
	}

	runtimeConfig, err := runtimecfg.LoadConfig("webhook-intake")
	if err != nil {
		return err
	}
	statusReader := postgres.NewInstrumentedStatusStore(postgres.SQLQueryer{DB: db}, instruments)
	service, err := newIntakeApplication(runtimeConfig, statusReader, intakeServer.Mux(), providers.PrometheusHandler)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return service.Run(ctx)
}

// openTriggerStores opens the five Postgres trigger stores and runs
// EnsureSchema on each, returning the bundle the intake API serves.
// Every error aborts startup: serving intake without durable storage would
// turn satellite submissions into 500s the provider must redeliver.
func openTriggerStores(parent context.Context, db *sql.DB, tracer trace.Tracer, instruments *telemetry.Instruments) (intake.Stores, error) {
	instrumented := func(storeName string) *postgres.InstrumentedDB {
		return &postgres.InstrumentedDB{
			Inner:       postgres.SQLDB{DB: db},
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   storeName,
		}
	}

	repository := webhookstore.NewWebhookTriggerStore(instrumented("webhook_triggers"))
	if err := repository.EnsureSchema(parent); err != nil {
		return intake.Stores{}, fmt.Errorf("webhook trigger schema: %w", err)
	}
	actions := actionsstore.NewStore(instrumented("cicd_actions_deliveries"))
	if err := actions.EnsureSchema(parent); err != nil {
		return intake.Stores{}, fmt.Errorf("actions delivery schema: %w", err)
	}
	incident := incidentfreshnessstore.NewIncidentFreshnessStore(instrumented("incident_freshness_triggers"))
	if err := incident.EnsureSchema(parent); err != nil {
		return intake.Stores{}, fmt.Errorf("incident freshness schema: %w", err)
	}
	aws := awsfreshnessstore.NewAWSFreshnessStore(instrumented("aws_freshness_triggers"))
	if err := aws.EnsureSchema(parent); err != nil {
		return intake.Stores{}, fmt.Errorf("aws freshness schema: %w", err)
	}
	gcp := gcpfreshnessstore.NewGCPFreshnessStore(instrumented("gcp_freshness_triggers"))
	if err := gcp.EnsureSchema(parent); err != nil {
		return intake.Stores{}, fmt.Errorf("gcp freshness schema: %w", err)
	}
	return intake.Stores{
		Repository: repository,
		Incident:   incident,
		AWS:        aws,
		GCP:        gcp,
		Actions:    actions,
	}, nil
}

// buildIntakeServer constructs core's authenticated versioned intake API
// over the given stores. An empty Bearer fails closed inside
// intake.NewServerChecked: an intake API without authentication must never
// serve.
func buildIntakeServer(cfg webhookIntakeConfig, stores intake.Stores, logger *slog.Logger) (*intake.Server, error) {
	return intake.NewServerChecked(intake.ServerConfig{
		Token:        strings.TrimSpace(cfg.IntakeToken),
		MaxBodyBytes: cfg.MaxRequestBodyBytes,
		Logger:       logger,
	}, stores)
}

func newIntakeApplication(
	runtimeConfig runtimecfg.Config,
	statusReader statuspkg.Reader,
	intakeMux http.Handler,
	prometheusHandler http.Handler,
) (app.Application, error) {
	adminMux, err := runtimecfg.NewStatusAdminMux(
		runtimeConfig.ServiceName,
		statusReader,
		intakeMux,
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
