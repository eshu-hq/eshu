package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var (
	_ query.GraphQuery   = (*query.Neo4jReader)(nil)
	_ query.ContentStore = (*query.ContentReader)(nil)
)

func wireAPI(
	ctx context.Context,
	getenv func(string) string,
	logger *slog.Logger,
	prometheusHandler http.Handler,
) (http.Handler, func(), error) {
	queryProfile, err := loadQueryProfile(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("load query profile: %w", err)
	}
	graphBackend, err := loadGraphBackend(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("load graph backend: %w", err)
	}
	semanticProviderProfiles, err := semanticprofile.LoadStatusesFromEnv(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("load semantic provider profiles: %w", err)
	}
	semanticPolicy, err := semanticpolicy.LoadFromEnv(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("load semantic extraction policy: %w", err)
	}
	semanticProviderProfiles = semanticpolicy.ApplyToProviderStatuses(
		semanticProviderProfiles,
		semanticPolicy,
	)

	apiKey, err := internalruntime.ResolveAPIKey(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve api key: %w", err)
	}

	driver, neo4jDB, err := openQueryGraph(ctx, getenv, queryProfile, logger)
	if err != nil {
		return nil, nil, err
	}

	// Open Postgres using pgx driver
	pgDSN := envOrDefault(getenv, "ESHU_POSTGRES_DSN",
		envOrDefault(getenv, "ESHU_CONTENT_STORE_DSN", ""))
	if pgDSN == "" {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("ESHU_POSTGRES_DSN or ESHU_CONTENT_STORE_DSN is required")
	}

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("ping postgres: %w", err)
	}
	if logger != nil {
		logger.Info("postgres connected", telemetry.EventAttr("runtime.postgres.connected"))
	}

	// Build query layer
	neo4jReader := query.NewNeo4jReader(driver, neo4jDB)
	contentReader := query.NewContentReader(db)
	statusReader := status.WithSemanticProviderProfiles(
		pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db}),
		semanticProviderProfiles...,
	)
	metricsSource, err := metricsTimeSeriesSourceFromEnv(getenv, nil)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("configure metrics time-series source: %w", err)
	}
	instruments, err := telemetry.NewInstruments(otel.Meter("eshu-api"))
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("register query instruments: %w", err)
	}
	router, err := newRouter(
		db,
		neo4jReader,
		contentReader,
		statusReader,
		metricsSource,
		queryProfile,
		graphBackend,
		logger,
		instruments,
	)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("new router: %w", err)
	}

	apiMux := http.NewServeMux()
	router.Mount(apiMux)

	mux, err := mountRuntimeSurface(apiMux, "eshu-api", statusReader, prometheusHandler)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("mount runtime surface: %w", err)
	}

	// Wrap with auth middleware
	authedMux := query.AuthMiddleware(apiKey, mux)

	cleanup := func() {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(context.Background())
		}
	}

	return authedMux, cleanup, nil
}

func openQueryGraph(
	ctx context.Context,
	getenv func(string) string,
	queryProfile query.QueryProfile,
	logger *slog.Logger,
) (neo4jdriver.DriverWithContext, string, error) {
	neo4jDB := envOrDefault(getenv, "DEFAULT_DATABASE", "nornic")
	if queryProfile == query.ProfileLocalLightweight || strings.EqualFold(envOrDefault(getenv, "ESHU_DISABLE_NEO4J", ""), "true") {
		return nil, neo4jDB, nil
	}

	driver, cfg, err := internalruntime.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return nil, "", err
	}
	if logger != nil {
		logger.Info("neo4j connected", telemetry.EventAttr("runtime.neo4j.connected"), slog.String("neo4j_uri", cfg.URI))
	}
	return driver, cfg.DatabaseName, nil
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func loadQueryProfile(getenv func(string) string) (query.QueryProfile, error) {
	raw := strings.TrimSpace(getenv("ESHU_QUERY_PROFILE"))
	if raw == "" {
		return query.ProfileProduction, nil
	}
	profile, err := query.ParseQueryProfile(raw)
	if err != nil {
		return "", err
	}
	return profile, nil
}

func loadGraphBackend(getenv func(string) string) (query.GraphBackend, error) {
	return query.ParseGraphBackend(strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND")))
}

func newRouter(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	statusReader status.Reader,
	metricsSource query.MetricsTimeSeriesSource,
	queryProfile query.QueryProfile,
	graphBackend query.GraphBackend,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) (*query.APIRouter, error) {
	if statusReader == nil {
		statusReader = pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db})
	}
	var containerImageIdentities query.ContainerImageIdentityStore
	var sbomAttachments query.SBOMAttestationAttachmentStore
	if db != nil {
		containerImageIdentities = query.NewPostgresContainerImageIdentityStore(db)
		sbomAttachments = query.NewPostgresSBOMAttestationAttachmentStore(db)
	}
	router := &query.APIRouter{
		Repositories: &query.RepositoryHandler{
			Neo4j:               neo4jReader,
			Content:             contentReader,
			CICDRunCorrelations: query.NewPostgresCICDRunCorrelationStore(db),
			Profile:             queryProfile,
			Logger:              logger,
		},
		Entities: &query.EntityHandler{
			Neo4j:                    neo4jReader,
			Content:                  contentReader,
			CICDRunCorrelations:      query.NewPostgresCICDRunCorrelationStore(db),
			ContainerImageIdentities: containerImageIdentities,
			SBOMAttachments:          sbomAttachments,
			Profile:                  queryProfile,
			Logger:                   logger,
		},
		Code: &query.CodeHandler{
			GraphBackend: graphBackend,
			Neo4j:        neo4jReader,
			Content:      contentReader,
			Profile:      queryProfile,
		},
		Content: &query.ContentHandler{
			Content: contentReader,
			Profile: queryProfile,
		},
		Infra: &query.InfraHandler{
			Neo4j:      neo4jReader,
			Aggregates: query.NewGraphInfraResourceAggregateStore(neo4jReader),
			Profile:    queryProfile,
		},
		IaC: &query.IaCHandler{
			Content:      contentReader,
			Reachability: query.NewPostgresIaCReachabilityStore(db),
			Management:   query.NewPostgresIaCManagementStore(db),
			Graph:        neo4jReader,
			Profile:      queryProfile,
		},
		Impact: &query.ImpactHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
			Logger:  logger,
		},
		Evidence: &query.EvidenceHandler{
			Content: contentReader,
			Profile: queryProfile,
		},
		Documentation: &query.DocumentationHandler{
			Content:    contentReader,
			Aggregates: query.NewPostgresDocumentationFindingAggregateStore(db),
			Profile:    queryProfile,
		},
		SemanticEvidence: &query.SemanticEvidenceHandler{
			Content: contentReader,
			Profile: queryProfile,
		},
		PackageRegistry: &query.PackageRegistryHandler{
			Neo4j:        neo4jReader,
			Content:      contentReader,
			Correlations: query.NewPostgresPackageRegistryCorrelationStore(db),
			Aggregates:   query.NewGraphPackageRegistryAggregateStore(neo4jReader),
			Profile:      queryProfile,
		},
		Dependencies: &query.DependenciesHandler{
			Neo4j:       neo4jReader,
			Profile:     queryProfile,
			Instruments: instruments,
		},
		CICD: &query.CICDHandler{
			Content:      contentReader,
			Correlations: query.NewPostgresCICDRunCorrelationStore(db),
			Aggregates:   query.NewPostgresCICDRunCorrelationAggregateStore(db),
			Profile:      queryProfile,
		},
		ServiceCatalog: &query.ServiceCatalogHandler{
			Content:      contentReader,
			Correlations: query.NewPostgresServiceCatalogCorrelationStore(db),
			Profile:      queryProfile,
		},
		Kubernetes: &query.KubernetesHandler{
			Correlations: query.NewPostgresKubernetesCorrelationStore(db),
			Profile:      queryProfile,
		},
		SecretsIAM: &query.SecretsIAMHandler{
			IdentityTrustChains:          query.NewPostgresSecretsIAMIdentityTrustChainStore(db),
			PrivilegePostureObservations: query.NewPostgresSecretsIAMPrivilegePostureObservationStore(db),
			SecretAccessPaths:            query.NewPostgresSecretsIAMSecretAccessPathStore(db),
			PostureGaps:                  query.NewPostgresSecretsIAMPostureGapStore(db),
			Summary:                      query.NewPostgresSecretsIAMPostureSummaryStore(db),
			Profile:                      queryProfile,
		},
		ObservabilityCoverage: &query.ObservabilityCoverageHandler{
			Content:      contentReader,
			Correlations: query.NewPostgresObservabilityCoverageCorrelationStore(db),
			Profile:      queryProfile,
		},
		Images: &query.ImageHandler{
			Neo4j:   neo4jReader,
			Profile: queryProfile,
		},
		SupplyChain: &query.SupplyChainHandler{
			Neo4j:                    neo4jReader,
			Content:                  contentReader,
			SBOMAttachments:          query.NewPostgresSBOMAttestationAttachmentStore(db),
			SBOMAttachmentAggregates: query.NewPostgresSBOMAttestationAttachmentAggregateStore(db),
			AdvisoryEvidence:         query.NewPostgresAdvisoryEvidenceStore(db),
			AdvisoryCatalog:          query.NewPostgresAdvisoryCatalogStore(db),
			ImpactFindings:           query.NewPostgresSupplyChainImpactFindingStore(db),
			ImpactAggregates:         query.NewPostgresSupplyChainImpactAggregateStore(db),
			ImpactExplanations:       query.NewPostgresSupplyChainImpactFindingStore(db),
			ContainerImageIdentities: query.NewPostgresContainerImageIdentityStore(db),
			ContainerImageAggregates: query.NewPostgresContainerImageIdentityAggregateStore(db),
			SecurityAlerts:           query.NewPostgresSecurityAlertReconciliationStore(db),
			SecurityAlertAggregates:  query.NewPostgresSecurityAlertReconciliationAggregateStore(db),
			Readiness:                query.NewPostgresSupplyChainImpactReadinessStore(db),
			Profile:                  queryProfile,
		},
		Incident: &query.IncidentHandler{
			Context: query.NewPostgresIncidentContextStore(db),
			Profile: queryProfile,
		},
		WorkItems: &query.WorkItemHandler{
			Evidence: query.NewPostgresWorkItemEvidenceStore(db),
			Profile:  queryProfile,
		},
		Status: &query.StatusHandler{
			Neo4j:        neo4jReader,
			DB:           db,
			StatusReader: statusReader,
			Profile:      queryProfile,
		},
		Metrics: &query.MetricsHandler{
			Source:  metricsSource,
			Profile: queryProfile,
		},
		Compare: &query.CompareHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
		},
		Admin: &query.AdminHandler{
			Store: query.NewPostgresAdminStore(db),
		},
	}
	if db == nil {
		return router, nil
	}

	recoveryHandler, err := recovery.NewHandler(pgstatus.NewRecoveryStore(pgstatus.SQLDB{DB: db}))
	if err != nil {
		return nil, fmt.Errorf("new recovery handler: %w", err)
	}
	reindexer, err := internalruntime.NewStatusRequestHandler(pgstatus.NewStatusRequestStore(pgstatus.SQLDB{DB: db}))
	if err != nil {
		return nil, fmt.Errorf("new status request handler: %w", err)
	}
	router.Admin.Recovery = recoveryHandler
	router.Admin.Reindexer = reindexer
	return router, nil
}

func mountRuntimeSurface(
	apiHandler http.Handler,
	serviceName string,
	reader status.Reader,
	prometheusHandler http.Handler,
) (http.Handler, error) {
	adminMux, err := internalruntime.NewStatusAdminMux(
		serviceName,
		reader,
		apiHandler,
		internalruntime.WithPrometheusHandler(prometheusHandler),
	)
	if err != nil {
		return nil, err
	}
	return adminMux, nil
}
