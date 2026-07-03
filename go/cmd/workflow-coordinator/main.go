// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	// Blank import populates the AWS scanner registry so coordinator-side
	// SupportsServiceKind checks accept every service the collector ships.
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
	"github.com/eshu-hq/eshu/go/internal/coordinator"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-workflow-coordinator"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		slog.Error("workflow coordinator failed", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	bootstrap, err := telemetry.NewBootstrap("workflow-coordinator")
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

	logger := telemetry.NewLogger(bootstrap, "workflow-coordinator", "workflow-coordinator")
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
	defer func() { _ = db.Close() }()

	cfg, err := coordinator.LoadConfig(os.Getenv)
	if err != nil {
		return err
	}
	semanticWorkerCfg, err := coordinator.LoadSemanticProviderWorkerConfig(os.Getenv)
	if err != nil {
		return err
	}

	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	metrics, err := coordinator.NewMetrics(meter)
	if err != nil {
		return fmt.Errorf("coordinator metrics: %w", err)
	}
	semanticWorkerMetrics, err := coordinator.NewSemanticProviderWorkerMetrics(meter)
	if err != nil {
		return fmt.Errorf("semantic provider worker metrics: %w", err)
	}
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	store := newWorkflowControlStore(postgres.SQLDB{DB: db}, instruments)
	tenantGrantDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "tenant_workspace_grants",
	}
	tenantGrantStore := postgres.NewTenantWorkspaceGrantStore(tenantGrantDB)
	if err := tenantGrantStore.EnsureSchema(parent); err != nil {
		return err
	}
	governanceAuditDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "governance_audit",
	}
	governanceAuditStore := postgres.NewGovernanceAuditStore(governanceAuditDB)
	if err := governanceAuditStore.EnsureSchema(parent); err != nil {
		return err
	}
	awsFreshnessDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "aws_freshness_triggers",
	}
	awsFreshnessStore := postgres.NewAWSFreshnessStore(awsFreshnessDB)
	if err := awsFreshnessStore.EnsureSchema(parent); err != nil {
		return err
	}
	gcpFreshnessDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "gcp_freshness_triggers",
	}
	gcpFreshnessStore := postgres.NewGCPFreshnessStore(gcpFreshnessDB)
	if err := gcpFreshnessStore.EnsureSchema(parent); err != nil {
		return err
	}
	incidentFreshnessDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "incident_freshness_triggers",
	}
	incidentFreshnessStore := postgres.NewIncidentFreshnessStore(incidentFreshnessDB)
	if err := incidentFreshnessStore.EnsureSchema(parent); err != nil {
		return err
	}
	ownedPackageTargetsDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "owned_package_targets",
	}
	installedAdvisoryTargetsDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "installed_advisory_targets",
	}
	factStore := postgres.NewFactStore(installedAdvisoryTargetsDB)
	serviceRunner := coordinator.Service{
		Config: cfg,
		Store:  store,
		TerraformStatePlanner: coordinator.TerraformStateWorkPlanner{
			GitReadiness: postgres.TerraformStateGitReadinessChecker{DB: postgres.SQLQueryer{DB: db}},
			BackendFacts: postgres.TerraformStateBackendFactReader{DB: postgres.SQLQueryer{DB: db}},
		},
		OCIRegistryPlanner:                coordinator.OCIRegistryWorkPlanner{},
		PackageRegistryPlanner:            coordinator.PackageRegistryWorkPlanner{},
		VulnerabilityIntelligencePlanner:  coordinator.VulnerabilityIntelligenceWorkPlanner{},
		SBOMAttestationPlanner:            coordinator.SBOMAttestationWorkPlanner{},
		ScannerWorkerPlanner:              coordinator.ScannerWorkerWorkPlanner{},
		SecurityAlertPlanner:              coordinator.SecurityAlertWorkPlanner{},
		CICDRunPlanner:                    coordinator.CICDRunWorkPlanner{},
		PagerDutyPlanner:                  coordinator.PagerDutyWorkPlanner{},
		JiraPlanner:                       coordinator.JiraWorkPlanner{},
		PrometheusMimirPlanner:            coordinator.PrometheusMimirWorkPlanner{},
		TempoPlanner:                      coordinator.TempoWorkPlanner{},
		GCPPlanner:                        coordinator.GCPWorkPlanner{},
		GrafanaPlanner:                    coordinator.GrafanaWorkPlanner{},
		LokiPlanner:                       coordinator.LokiWorkPlanner{},
		VaultLivePlanner:                  coordinator.VaultLiveWorkPlanner{},
		ComponentExtensionPlanner:         coordinator.ComponentExtensionWorkPlanner{},
		OwnedPackageTargetReader:          postgres.NewFactStore(ownedPackageTargetsDB),
		TenantGrantReader:                 tenantGrantReader{store: tenantGrantStore},
		OSPackageAdvisoryTargetReader:     factStore,
		SBOMComponentAdvisoryTargetReader: factStore,
		AWSScheduledPlanner:               coordinator.AWSScheduledWorkPlanner{},
		AWSFreshnessTriggers:              awsFreshnessStore,
		AWSFreshnessPlanner:               coordinator.AWSFreshnessWorkPlanner{},
		AWSFreshnessEvents:                instruments.AWSFreshnessEvents,
		GCPFreshnessTriggers:              gcpFreshnessStore,
		GCPFreshnessEvents:                instruments.GCPFreshnessEvents,
		GCPFreshnessFanOut:                instruments.GCPFreshnessFanOut,
		IncidentFreshnessTriggers:         incidentFreshnessStore,
		GovernanceAudit:                   governanceAuditStore,
		Metrics:                           metrics,
		Logger:                            logger,
	}
	if semanticWorkerCfg.Enabled {
		// Default no-network client: real outbound provider traffic is intentionally
		// not wired here. A concrete enabled client is supplied by a future,
		// security-reviewed PR. With this default the worker only claims, gates
		// egress, audits decisions, and terminates allowed jobs as provider-disabled.
		serviceRunner.SemanticProviderWorker = &coordinator.SemanticProviderWorker{
			Config:          semanticWorkerCfg,
			Claimer:         postgres.NewSemanticExtractionQueueStore(postgres.SQLDB{DB: db}),
			Client:          coordinator.DisabledSemanticProviderClient{},
			GovernanceAudit: governanceAuditStore,
			Metrics:         semanticWorkerMetrics,
			Logger:          logger,
		}
		logger.Info(
			"semantic provider execution worker enabled",
			"execution_enabled", semanticWorkerCfg.ExecutionEnabled,
			"scope_count", len(semanticWorkerCfg.ScopeIDs),
		)
	}
	statusReader := postgres.NewStatusStore(postgres.SQLQueryer{DB: db})
	service, err := app.NewHostedWithStatusServer(
		"workflow-coordinator",
		serviceRunner,
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
