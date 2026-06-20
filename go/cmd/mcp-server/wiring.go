package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
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

func wireAPI(
	ctx context.Context,
	getenv func(string) string,
	logger *slog.Logger,
	prometheusHandler http.Handler,
) (http.Handler, *http.ServeMux, func(), error) {
	queryProfile, err := loadQueryProfile(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load query profile: %w", err)
	}
	graphBackend, err := loadGraphBackend(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load graph backend: %w", err)
	}
	semanticProviderProfiles, err := semanticprofile.LoadStatusesFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load semantic provider profiles: %w", err)
	}
	semanticPolicy, err := semanticpolicy.LoadFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load semantic extraction policy: %w", err)
	}
	semanticSearchEmbedding, err := searchembedruntime.ConfigFromEnv(getenv, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load semantic search embedder: %w", err)
	}
	semanticProviderProfiles = semanticpolicy.ApplyToProviderStatuses(
		semanticProviderProfiles,
		semanticPolicy,
	)

	apiKey, err := internalruntime.ResolveAPIKey(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve api key: %w", err)
	}
	scopedTokenResolver, err := scopedtoken.ResolverFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve scoped token registry: %w", err)
	}
	governanceStatus := query.GovernanceStatusConfigFromEnv(getenv, apiKey != "")

	driver, neo4jDB, err := openQueryGraph(ctx, getenv, queryProfile, logger)
	if err != nil {
		return nil, nil, nil, err
	}

	// Open Postgres using pgx driver
	pgDSN := envOrDefault(getenv, "ESHU_POSTGRES_DSN",
		envOrDefault(getenv, "ESHU_CONTENT_STORE_DSN", ""))
	if pgDSN == "" {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("ESHU_POSTGRES_DSN or ESHU_CONTENT_STORE_DSN is required")
	}

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("ping postgres: %w", err)
	}
	if logger != nil {
		logger.Info("postgres connected", telemetry.EventAttr("runtime.postgres.connected"))
	}
	instruments, err := telemetry.NewInstruments(otel.Meter("mcp-server"))
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("register query instruments: %w", err)
	}

	// Build query layer
	neo4jReader := query.NewNeo4jReader(driver, neo4jDB)
	contentReader := query.NewContentReader(db)
	statusReader := status.WithSemanticProviderProfiles(
		pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db}),
		semanticProviderProfiles...,
	)
	governanceAudit := pgstatus.NewGovernanceAuditStore(pgstatus.SQLDB{DB: db})

	componentHome := strings.TrimSpace(getenv("ESHU_COMPONENT_HOME"))
	componentPolicy := componentPolicyFromEnv(getenv)
	router := newMCPQueryRouterWithSemanticEmbedding(
		db,
		neo4jReader,
		contentReader,
		statusReader,
		queryProfile,
		graphBackend,
		logger,
		instruments,
		semanticSearchEmbedding,
		componentHome,
		componentPolicy,
		governanceStatus,
		governanceAudit,
	)

	mux := http.NewServeMux()
	router.Mount(mux)

	// Mount the service intelligence report route so the get_service_intelligence_report
	// MCP tool dispatches to a real handler. It lives in its own package (importing
	// query and serviceintel) and is mounted here, not by the query router, so query
	// never depends on serviceintel — no cycle. The incidents_support section is
	// sourced from durable incident-routing evidence over Postgres.
	(&serviceintelhttp.ReportHandler{
		Entities:    router.Entities,
		Incidents:   newIncidentEvidenceSource(db, logger),
		SupplyChain: newSupplyChainEvidenceSource(db, logger),
	}).Mount(mux)

	// Mount POST /api/v0/ask so the MCP "ask" tool dispatch does not 404.
	// The handler is default-off (nil Asker → 503 state:"unavailable") when
	// ESHU_ASK_ENABLED is unset or no agent_reasoning provider profile is
	// configured. This matches the cmd/api wiring: the MCP server does not
	// provide an in-process engine, so the AskHandler always runs in
	// default-off mode here; the MCP ask tool proxies through HTTP.
	(&query.AskHandler{}).Mount(mux)

	// Record per-endpoint duration/error metrics for every read route, then wrap
	// with auth middleware (shared token + optional scoped-token registry;
	// protects all /api/v0/* routes when mounted by MCP server)
	instrumentedMux := query.RequestMetricsMiddleware(mux)
	authedHandler := query.AuthMiddlewareWithScopedTokensAndGovernanceAudit(apiKey, scopedTokenResolver, instrumentedMux, governanceAudit)

	adminMux, err := mountRuntimeSurface("mcp-server", statusReader, prometheusHandler, db, driver)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("mount runtime surface: %w", err)
	}

	cleanup := func() {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(context.Background())
		}
	}

	return authedHandler, adminMux, cleanup, nil
}

func newMCPQueryRouter(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	statusReader status.Reader,
	queryProfile query.QueryProfile,
	graphBackend query.GraphBackend,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	semanticSearchLocalEmbedder string,
	componentHome string,
	componentPolicy component.Policy,
	governanceStatus query.GovernanceStatusConfig,
	governanceAudit query.GovernanceAuditSummaryReader,
) *query.APIRouter {
	semanticSearchEmbedding, _ := searchembedruntime.ConfigFromEnv(func(key string) string {
		if key == envSemanticSearchLocalEmbedder {
			return semanticSearchLocalEmbedder
		}
		return ""
	}, nil)
	return newMCPQueryRouterWithSemanticEmbedding(
		db,
		neo4jReader,
		contentReader,
		statusReader,
		queryProfile,
		graphBackend,
		logger,
		instruments,
		semanticSearchEmbedding,
		componentHome,
		componentPolicy,
		governanceStatus,
		governanceAudit,
	)
}

func newMCPQueryRouterWithSemanticEmbedding(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	statusReader status.Reader,
	queryProfile query.QueryProfile,
	graphBackend query.GraphBackend,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	semanticSearchEmbedding searchembedruntime.Config,
	componentHome string,
	componentPolicy component.Policy,
	governanceStatus query.GovernanceStatusConfig,
	governanceAudit query.GovernanceAuditSummaryReader,
) *query.APIRouter {
	if statusReader == nil {
		statusReader = pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db})
	}
	if governanceAudit == nil && db != nil {
		governanceAudit = pgstatus.NewGovernanceAuditStore(pgstatus.SQLDB{DB: db})
	}
	var containerImageIdentities query.ContainerImageIdentityStore
	var sbomAttachments query.SBOMAttestationAttachmentStore
	if db != nil {
		containerImageIdentities = query.NewPostgresContainerImageIdentityStore(db)
		sbomAttachments = query.NewPostgresSBOMAttestationAttachmentStore(db)
	}
	return &query.APIRouter{
		Repositories: &query.RepositoryHandler{
			Neo4j:               neo4jReader,
			Content:             contentReader,
			CICDRunCorrelations: query.NewPostgresCICDRunCorrelationStore(db),
			Profile:             queryProfile,
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
		PackageRegistry: &query.PackageRegistryHandler{
			Neo4j:        neo4jReader,
			Content:      contentReader,
			Correlations: query.NewPostgresPackageRegistryCorrelationStore(db),
			Aggregates:   query.NewGraphPackageRegistryAggregateStore(neo4jReader),
			Profile:      queryProfile,
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
		Visualization: &query.VisualizationHandler{},
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
		Capabilities:           &query.CapabilitiesHandler{Profile: queryProfile},
		SurfaceInventory:       &query.SurfaceInventoryHandler{Profile: queryProfile},
		Compare: &query.CompareHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
		},
	}
}
