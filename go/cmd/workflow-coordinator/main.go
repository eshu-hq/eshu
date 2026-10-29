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
	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	ociregistry "github.com/eshu-hq/eshu/go/internal/coordinator/oci/registry"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/aws/freshness"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/aws/scheduled"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/gcp"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/grafana"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/jira"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/loki"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/prometheus"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/tempo"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/tfstate"
	packages "github.com/eshu-hq/eshu/go/internal/coordinator/registry/package"
	"github.com/eshu-hq/eshu/go/internal/coordinator/sbom/attestation"
	scannerworker "github.com/eshu-hq/eshu/go/internal/coordinator/scanner/worker"
	"github.com/eshu-hq/eshu/go/internal/coordinator/security/alert"
	coordinatorsemantic "github.com/eshu-hq/eshu/go/internal/coordinator/semantic"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/aws"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/gcp"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/incident"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/work/items"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/retirement"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/semantic"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/tenant"
	actionsstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook/actions"
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

	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	// The registry readback and activation planning inside LoadConfig make the
	// coordinator's producer-grant decisions, so the observer is built from the
	// process telemetry runtime before the config loads (#7153).
	cfg, err := coordinator.LoadConfigObserved(os.Getenv, newGrantObserver(instruments, logger))
	if err != nil {
		return err
	}
	semanticWorkerCfg, err := coordinatorsemantic.LoadProviderWorkerConfig(os.Getenv)
	if err != nil {
		return err
	}

	coordinatorMetrics, err := coordinator.NewMetrics(meter)
	if err != nil {
		return fmt.Errorf("coordinator metrics: %w", err)
	}
	semanticWorkerMetrics, err := coordinatorsemantic.NewProviderWorkerMetrics(meter, coordinator.MetricPrefix)
	if err != nil {
		return fmt.Errorf("semantic provider worker metrics: %w", err)
	}
	// The quota lookup is built from the admitted manifests of components
	// planning at least one claims-enabled instance (#8582) with the
	// operator override applied per field (#8561): coordinator
	// LoadConfigObserved reads both, so cfg.QuotaLookup is always non-nil
	// here. With nothing to enforce every kind reports unthrottled and
	// claims keep the plain statement; manifest or override changes need a
	// restart, since the lookup is built once at startup.
	store := newWorkflowControlStore(postgres.SQLDB{DB: db}, instruments, cfg.QuotaLookup)
	tenantGrantDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "tenant_workspace_grants",
	}
	tenantGrantStore := tenantstore.NewTenantWorkspaceGrantStore(tenantGrantDB)
	if err := tenantGrantStore.EnsureSchema(parent); err != nil {
		return err
	}
	governanceAuditDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "governance_audit",
	}
	governanceAuditStore := auditstore.NewGovernanceAuditStore(governanceAuditDB)
	if err := governanceAuditStore.EnsureSchema(parent); err != nil {
		return err
	}
	awsFreshnessDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "aws_freshness_triggers",
	}
	awsFreshnessStore := awsfreshnessstore.NewAWSFreshnessStore(awsFreshnessDB)
	if err := awsFreshnessStore.EnsureSchema(parent); err != nil {
		return err
	}
	gcpFreshnessDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "gcp_freshness_triggers",
	}
	gcpFreshnessStore := gcpfreshnessstore.NewGCPFreshnessStore(gcpFreshnessDB)
	if err := gcpFreshnessStore.EnsureSchema(parent); err != nil {
		return err
	}
	incidentFreshnessDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "incident_freshness_triggers",
	}
	actionsDB := &postgres.InstrumentedDB{Inner: postgres.SQLDB{DB: db}, Tracer: providers.TracerProvider.Tracer(telemetry.DefaultSignalName), Instruments: instruments, StoreName: "cicd_actions_deliveries"}
	actionsStore := actionsstore.NewStore(actionsDB)
	if err := actionsStore.EnsureSchema(parent); err != nil {
		return err
	}
	incidentFreshnessStore := incidentfreshnessstore.NewIncidentFreshnessStore(incidentFreshnessDB)
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
	// The retention sweeper is always wired: retention is on by default in
	// active mode, and Service.Run fails when it is on without a store
	// (#8829). ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE=0 turns it off.
	// The security-alert planner reads the scopes org targets already have, to
	// retire the shard scopes a partition no longer plans (#9019).
	securityAlertShardScopesDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "security_alert_shard_scopes",
	}
	workItemRetentionDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "workflow_work_item_retention",
	}
	serviceRunner := coordinator.Service{
		Config: cfg,
		Store:  store,
		TerraformStatePlanner: tfstate.WorkPlanner{
			GitReadiness: postgres.TerraformStateGitReadinessChecker{DB: postgres.SQLQueryer{DB: db}},
			BackendFacts: postgres.TerraformStateBackendFactReader{DB: postgres.SQLQueryer{DB: db}},
		},
		OCIRegistryPlanner:                ociregistry.WorkPlanner{},
		PackageRegistryPlanner:            packages.WorkPlanner{},
		VulnerabilityIntelligencePlanner:  vulnerability.IntelligenceWorkPlanner{},
		SBOMAttestationPlanner:            attestation.WorkPlanner{},
		ScannerWorkerPlanner:              scannerworker.WorkPlanner{},
		SecurityAlertPlanner:              alert.WorkPlanner{ShardScopes: retirement.NewReader(securityAlertShardScopesDB), Logger: logger},
		CICDRunPlanner:                    cicdrun.WorkPlanner{},
		CICDRunActionsTriggers:            actionsStore,
		CICDRunInstruments:                instruments,
		PagerDutyPlanner:                  pagerduty.WorkPlanner{},
		JiraPlanner:                       jira.WorkPlanner{},
		PrometheusMimirPlanner:            prometheus.WorkPlanner{},
		TempoPlanner:                      tempo.WorkPlanner{},
		GCPPlanner:                        gcp.WorkPlanner{},
		GrafanaPlanner:                    grafana.WorkPlanner{},
		LokiPlanner:                       loki.WorkPlanner{},
		ComponentExtensionPlanner:         extension.WorkPlanner{},
		OwnedPackageTargetReader:          postgres.NewFactStore(ownedPackageTargetsDB),
		TenantGrantReader:                 tenantGrantReader{store: tenantGrantStore},
		OSPackageAdvisoryTargetReader:     factStore,
		SBOMComponentAdvisoryTargetReader: factStore,
		AWSScheduledPlanner:               scheduled.WorkPlanner{},
		AWSFreshnessTriggers:              awsFreshnessStore,
		AWSFreshnessPlanner:               freshness.WorkPlanner{},
		AWSFreshnessEvents:                instruments.AWSFreshnessEvents,
		GCPFreshnessTriggers:              gcpFreshnessStore,
		GCPFreshnessEvents:                instruments.GCPFreshnessEvents,
		GCPFreshnessFanOut:                instruments.GCPFreshnessFanOut,
		ActivationDerivedScopes:           instruments.ActivationDerivedScopesPlanned,
		IncidentFreshnessTriggers:         incidentFreshnessStore,
		GovernanceAudit:                   governanceAuditStore,
		Metrics:                           coordinatorMetrics,
		WorkItemRetention:                 items.NewSweeper(workItemRetentionDB),
		Logger:                            logger,
	}
	if semanticWorkerCfg.Enabled {
		// Default no-network client: real outbound provider traffic is intentionally
		// not wired here. A concrete enabled client is supplied by a future,
		// security-reviewed PR. With this default the worker only claims, gates
		// egress, audits decisions, and terminates allowed jobs as provider-disabled.
		serviceRunner.SemanticProviderWorker = &coordinatorsemantic.ProviderWorker{
			Config:          semanticWorkerCfg,
			Claimer:         semanticstore.NewSemanticExtractionQueueStore(postgres.SQLDB{DB: db}),
			Client:          coordinatorsemantic.DisabledProviderClient{},
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
	statusReader := postgres.NewInstrumentedStatusStore(postgres.SQLQueryer{DB: db}, instruments)
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
