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

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scopedtoken"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var (
	_ query.GraphQuery   = (*query.Neo4jReader)(nil)
	_ query.ContentStore = (*query.ContentReader)(nil)
)

const envSemanticSearchLocalEmbedder = searchembedruntime.EnvLocalEmbedder

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
	semanticSearchEmbedding, err := searchembedruntime.ConfigFromEnv(getenv, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("load semantic search embedder: %w", err)
	}
	semanticProviderProfiles = semanticpolicy.ApplyToProviderStatuses(
		semanticProviderProfiles,
		semanticPolicy,
	)

	apiKey, err := internalruntime.ResolveAPIKey(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve api key: %w", err)
	}
	scopedTokenResolver, err := scopedtoken.ResolverFromEnv(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve scoped token registry: %w", err)
	}
	governanceStatus := query.GovernanceStatusConfigFromEnv(getenv, apiKey != "")

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
	governanceAuditDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		governanceAuditDB = &pgstatus.InstrumentedDB{
			Inner:       governanceAuditDB,
			Tracer:      otel.Tracer("eshu-api"),
			Instruments: instruments,
			StoreName:   "governance_audit",
		}
	}
	governanceAudit := pgstatus.NewGovernanceAuditStore(governanceAuditDB)
	componentHome := strings.TrimSpace(getenv("ESHU_COMPONENT_HOME"))
	componentPolicy := componentPolicyFromEnv(getenv)
	readImpactFromWinners := query.SupplyChainImpactWinnersReadEnabled(getenv(query.SupplyChainImpactWinnersReadEnv))
	router, err := newRouterWithSemanticEmbedding(
		db,
		neo4jReader,
		contentReader,
		statusReader,
		metricsSource,
		queryProfile,
		graphBackend,
		logger,
		instruments,
		semanticSearchEmbedding,
		componentHome,
		componentPolicy,
		governanceStatus,
		governanceAudit,
		readImpactFromWinners,
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

	// The Ask engine's in-process runner must dispatch inner tool calls through
	// the scoped-auth-wrapped handler (authedMux below) so each inner read
	// re-runs the scoped-route gate under the caller's token. That handler does
	// not exist yet (it wraps this mux), so wire the runner to a deferred handler
	// now and install authedMux into it once built.
	askInnerHandler := &deferredHandler{}
	mountAskAndNarration(getenv, apiMux, askInnerHandler, apiKey, router.Status, logger)

	// Mount the service intelligence report route. It lives in its own package
	// (which imports both query and serviceintel) and is mounted here rather than
	// by the query router, so query never depends on serviceintel — no cycle. The
	// incidents_support section is sourced from durable incident-routing evidence
	// (catalog-service-id resolver + incident evidence loader, both over Postgres).
	(&serviceintelhttp.ReportHandler{
		Entities:    router.Entities,
		Incidents:   newIncidentEvidenceSource(db, logger),
		SupplyChain: newSupplyChainEvidenceSource(db, logger),
	}).Mount(apiMux)

	// Record per-endpoint duration/error metrics for every API route. The
	// middleware wraps the application mux only; the admin surface (probes,
	// /metrics) is mounted separately and is intentionally not counted.
	instrumentedAPI := query.RequestMetricsMiddleware(apiMux)

	mux, err := mountRuntimeSurface(instrumentedAPI, "eshu-api", statusReader, prometheusHandler, db, driver)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("mount runtime surface: %w", err)
	}

	// Wrap with auth middleware (shared token + optional scoped-token registry)
	authedMux := query.AuthMiddlewareWithScopedTokensAndGovernanceAudit(apiKey, scopedTokenResolver, mux, governanceAudit)

	// Install the fully-wrapped handler into the Ask runner's deferred handler so
	// inner tool dispatches re-run auth + the scoped-route gate under the caller's
	// token. Done before returning, hence before any request is served.
	askInnerHandler.Set(authedMux)

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
	semanticSearchLocalEmbedder string,
	componentHome string,
	componentPolicy component.Policy,
	governanceStatus query.GovernanceStatusConfig,
	governanceAudit query.GovernanceAuditSummaryReader,
	readImpactFromWinners bool,
) (*query.APIRouter, error) {
	semanticSearchEmbedding, err := searchembedruntime.ConfigFromEnv(func(key string) string {
		if key == envSemanticSearchLocalEmbedder {
			return semanticSearchLocalEmbedder
		}
		return ""
	}, nil)
	if err != nil {
		return nil, err
	}
	return newRouterWithSemanticEmbedding(
		db,
		neo4jReader,
		contentReader,
		statusReader,
		metricsSource,
		queryProfile,
		graphBackend,
		logger,
		instruments,
		semanticSearchEmbedding,
		componentHome,
		componentPolicy,
		governanceStatus,
		governanceAudit,
		readImpactFromWinners,
	)
}

func newRouterWithSemanticEmbedding(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	statusReader status.Reader,
	metricsSource query.MetricsTimeSeriesSource,
	queryProfile query.QueryProfile,
	graphBackend query.GraphBackend,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	semanticSearchEmbedding searchembedruntime.Config,
	componentHome string,
	componentPolicy component.Policy,
	governanceStatus query.GovernanceStatusConfig,
	governanceAudit query.GovernanceAuditSummaryReader,
	readImpactFromWinners bool,
) (*query.APIRouter, error) {
	if statusReader == nil {
		statusReader = pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db})
	}
	if governanceAudit == nil && db != nil {
		governanceAuditDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
		if instruments != nil {
			governanceAuditDB = &pgstatus.InstrumentedDB{
				Inner:       governanceAuditDB,
				Tracer:      otel.Tracer("eshu-api"),
				Instruments: instruments,
				StoreName:   "governance_audit",
			}
		}
		governanceAudit = pgstatus.NewGovernanceAuditStore(governanceAuditDB)
	}
	var containerImageIdentities query.ContainerImageIdentityStore
	var sbomAttachments query.SBOMAttestationAttachmentStore
	if db != nil {
		containerImageIdentities = query.NewPostgresContainerImageIdentityStore(db)
		sbomAttachments = query.NewPostgresSBOMAttestationAttachmentStore(db)
	}
	router := &query.APIRouter{
		Repositories: &query.RepositoryHandler{
			Neo4j:                      neo4jReader,
			Content:                    contentReader,
			CICDRunCorrelations:        query.NewPostgresCICDRunCorrelationStore(db),
			ServiceCatalogCorrelations: query.NewPostgresServiceCatalogCorrelationStore(db),
			Profile:                    queryProfile,
			Logger:                     logger,
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
		GraphEntityInventory: &query.GraphEntityInventoryHandler{
			Neo4j:   neo4jReader,
			Profile: queryProfile,
		},
		CloudInventory: &query.CloudInventoryHandler{
			Content: contentReader,
			Profile: queryProfile,
		},
		CloudRuntimeDrift: &query.CloudRuntimeDriftHandler{
			Store:   query.NewPostgresMultiCloudRuntimeDriftStore(db),
			Profile: queryProfile,
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
			Content:            contentReader,
			AdmissionDecisions: query.NewPostgresAdmissionDecisionReadStore(pgstatus.SQLDB{DB: db}),
			Profile:            queryProfile,
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
		SemanticSearch: &query.SemanticSearchHandler{
			Index:       query.NewPostgresSemanticSearchIndexStore(db),
			LocalHybrid: newSemanticSearchHybrid(db, semanticSearchEmbedding, instruments),
			Profile:     queryProfile,
		},
		PackageRegistry: newPackageRegistryHandler(db, neo4jReader, contentReader, queryProfile),
		Dependencies: &query.DependenciesHandler{
			Neo4j:       neo4jReader,
			Profile:     queryProfile,
			Instruments: instruments,
		},
		CICD:                  newCICDHandler(db, contentReader, queryProfile),
		ServiceCatalog:        newServiceCatalogHandler(db, contentReader, queryProfile),
		Kubernetes:            newKubernetesHandler(db, queryProfile),
		SecretsIAM:            newSecretsIAMHandler(db, queryProfile),
		ObservabilityCoverage: newObservabilityCoverageHandler(db, contentReader, queryProfile),
		Images: &query.ImageHandler{
			Neo4j:   neo4jReader,
			Profile: queryProfile,
		},
		SupplyChain:   newSupplyChainHandler(db, neo4jReader, contentReader, queryProfile, readImpactFromWinners),
		Incident:      newIncidentHandler(db, queryProfile),
		WorkItems:     newWorkItemHandler(db, queryProfile),
		Visualization: &query.VisualizationHandler{},
		Freshness:     newFreshnessHandler(db, queryProfile),
		Status: &query.StatusHandler{
			Neo4j:           neo4jReader,
			DB:              db,
			StatusReader:    statusReader,
			GovernanceAudit: governanceAudit,
			Profile:         queryProfile,
			Governance:      governanceStatus,
		},
		ComponentExtensions: &query.ComponentExtensionsHandler{
			ComponentHome: componentHome,
			Policy:        componentPolicy,
			Profile:       queryProfile,
		},
		ExtractionReadiness:    &query.CollectorExtractionReadinessHandler{Profile: queryProfile},
		FactSchemaVersions:     &query.FactSchemaVersionHandler{Profile: queryProfile},
		Playbooks:              &query.QueryPlaybookHandler{Profile: queryProfile},
		InvestigationWorkflows: &query.InvestigationWorkflowHandler{Profile: queryProfile},
		Metrics: &query.MetricsHandler{
			Source:  metricsSource,
			Profile: queryProfile,
		},
		Capabilities:     &query.CapabilitiesHandler{Profile: queryProfile},
		SurfaceInventory: &query.SurfaceInventoryHandler{Profile: queryProfile},
		Compare: &query.CompareHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
		},
		Admin: &query.AdminHandler{
			Store: query.NewPostgresAdminStore(db),
			Audit: adminRecoveryAuditAppender(governanceAudit),
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
