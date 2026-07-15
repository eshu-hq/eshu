// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	// Validate the Postgres pool config before dialing any datastore, so an invalid
	// ESHU_POSTGRES_MAX_OPEN_CONNS/idle/lifetime is reported regardless of graph
	// backend availability (validation-before-datastore invariant). It is applied
	// after sql.Open below.
	pgPoolCfg, err := internalruntime.LoadPostgresConfig(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load postgres pool config: %w", err)
	}

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
	// Bound the pool to the shared per-process ceiling (validated above, before the
	// graph dial). Without this the mcp-server pool is database/sql-default
	// unbounded, which would let a read burst exceed the whole-stack connection
	// budget (#4456). Only the pool sizes are applied; the DSN resolved above is kept.
	internalruntime.ConfigurePostgresPool(db, pgPoolCfg)
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
	scopedTokenResolver = scopedtoken.ChainResolvers(
		scopedtoken.NewPostgresIdentityResolver(pgstatus.NewScopedAPITokenStore(pgstatus.SQLDB{DB: db})),
		scopedTokenResolver,
	)
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
		newStatusStore(pgstatus.SQLQueryer{DB: db}, instruments),
		semanticProviderProfiles...,
	)
	governanceAudit := pgstatus.NewGovernanceAuditStore(pgstatus.SQLDB{DB: db})

	componentHome := strings.TrimSpace(getenv("ESHU_COMPONENT_HOME"))
	componentPolicy := componentPolicyFromEnv(getenv)
	readImpactFromWinners := query.SupplyChainImpactWinnersReadEnabled(getenv(query.SupplyChainImpactWinnersReadEnv))
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
		readImpactFromWinners,
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

	// Mount POST /api/v0/ask and wire the governed narration posture. The engine
	// uses the MCP server's own in-process mux as the MCPRunner handler so the
	// engine's tool calls dispatch through this server's routes — the same
	// pattern as cmd/api. Default-off when ESHU_ASK_ENABLED is unset or no
	// agent_reasoning provider profile is configured.
	mountAskAndNarration(getenv, mux, apiKey, router.Status, logger)

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
	readImpactFromWinners bool,
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
		readImpactFromWinners,
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
	readImpactFromWinners bool,
) *query.APIRouter {
	if statusReader == nil {
		statusReader = newStatusStore(pgstatus.SQLQueryer{DB: db}, instruments)
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
			Neo4j:                      neo4jReader,
			Content:                    contentReader,
			CICDRunCorrelations:        query.NewPostgresCICDRunCorrelationStore(db),
			ServiceCatalogCorrelations: query.NewPostgresServiceCatalogCorrelationStore(db),
			// Freshness backs get_repository_freshness (#5143). It must be
			// wired here (mirroring cmd/api/wiring_router.go) or the
			// advertised MCP tool 503s with "repository freshness reader
			// not configured" on the standalone MCP server, even though
			// GET /api/v0/repositories/{id}/freshness works on cmd/api --
			// the B-7 golden-corpus gate's MCP query-truth phase asserts
			// this tool live against this binary.
			Freshness: pgstatus.NewInstrumentedRepositoryFreshnessStore(pgstatus.SQLQueryer{DB: db}, instruments),
			Profile:   queryProfile,
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
			CodeFlow:     query.NewPostgresCodeFlowStore(db),
			Profile:      queryProfile,
			HybridRanker: newCodeHybridRanker(semanticSearchEmbedding),
		},
		Content: &query.ContentHandler{
			Content:      contentReader,
			Profile:      queryProfile,
			HybridRanker: newContentHybridRanker(semanticSearchEmbedding),
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
			Index:         query.NewPostgresSemanticSearchIndexStore(db),
			LocalHybrid:   newSemanticSearchHybrid(db, semanticSearchEmbedding, instruments),
			ScopeResolver: newInstrumentedSemanticSearchScopeResolver(db, instruments),
			Profile:       queryProfile,
			SearchVectorReady: query.NewPostgresSearchVectorReadyStore(db, query.SearchVectorBuildIdentity{
				ProviderProfileID:  semanticSearchEmbedding.ProviderProfileID,
				SourceClass:        semanticSearchEmbedding.SourceClass,
				EmbeddingModelID:   semanticSearchEmbedding.EmbeddingModelID,
				VectorIndexVersion: semanticSearchEmbedding.VectorIndexVersion,
			}),
		},
		PackageRegistry: &query.PackageRegistryHandler{
			Neo4j:              neo4jReader,
			Content:            contentReader,
			Correlations:       query.NewPostgresPackageRegistryCorrelationStore(db),
			Aggregates:         query.NewGraphPackageRegistryAggregateStore(neo4jReader),
			CollectorReadiness: query.NewPostgresCollectorListReadinessStore(db),
			Profile:            queryProfile,
		},
		CICD: &query.CICDHandler{
			Content:            contentReader,
			Correlations:       query.NewPostgresCICDRunCorrelationStore(db),
			Aggregates:         query.NewPostgresCICDRunCorrelationAggregateStore(db),
			CollectorReadiness: query.NewPostgresCollectorListReadinessStore(db),
			Profile:            queryProfile,
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
		CloudRuntimeDrift: &query.CloudRuntimeDriftHandler{
			Store:   query.NewPostgresMultiCloudRuntimeDriftStore(db),
			Profile: queryProfile,
		},
		SupplyChain: &query.SupplyChainHandler{
			Neo4j:                    neo4jReader,
			Content:                  contentReader,
			SBOMAttachments:          query.NewPostgresSBOMAttestationAttachmentStore(db),
			SBOMAttachmentAggregates: query.NewPostgresSBOMAttestationAttachmentAggregateStore(db),
			AdvisoryEvidence:         query.NewPostgresAdvisoryEvidenceStore(db),
			AdvisoryCatalog:          query.NewPostgresAdvisoryCatalogStore(db),
			ImpactFindings: query.NewPostgresSupplyChainImpactFindingStoreWithReadModel(
				db, readImpactFromWinners,
			),
			ImpactAggregates:         query.NewPostgresSupplyChainImpactAggregateStore(db),
			ImpactExplanations:       query.NewPostgresSupplyChainImpactFindingStore(db),
			ContainerImageIdentities: query.NewPostgresContainerImageIdentityStore(db),
			ContainerImageAggregates: query.NewPostgresContainerImageIdentityAggregateStore(db),
			SecurityAlerts:           query.NewPostgresSecurityAlertReconciliationStore(db),
			SecurityAlertAggregates:  query.NewPostgresSecurityAlertReconciliationAggregateStore(db),
			Readiness:                query.NewPostgresSupplyChainImpactReadinessStore(db),
			CollectorReadiness:       query.NewPostgresCollectorListReadinessStore(db),
			Profile:                  queryProfile,
		},
		Incident: &query.IncidentHandler{
			Context: query.NewPostgresIncidentContextStore(db),
			// Authorizer gates scoped-token reads of get_incident_context (#2144:
			// "Authorize ... the get_incident_context MCP tool ... for scoped
			// tokens"). It was wired only in cmd/api's newIncidentHandler
			// (wiring_handlers.go), so a scoped token calling get_incident_context
			// on the standalone MCP server always got a fail-closed not-found --
			// found by the #5148 dual-main reflective completeness test
			// (TestNewMCPQueryRouterWiresEveryFieldOrDocumentsWhyNot below), which
			// flags any nil interface field inside a wired handler.
			Authorizer: query.NewPostgresIncidentRepositoryAuthorizer(db),
			Profile:    queryProfile,
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
		Freshness: &query.FreshnessHandler{
			Generations:         pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db}),
			ChangedSince:        pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db}),
			ServiceChangedSince: pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db}),
			Profile:             queryProfile,
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
		AdminDeadLetters: &query.AdminDeadLetterListHandler{
			Store: query.NewPostgresAdminStore(db),
		},
		AdminInputInvalidFacts: &query.AdminInputInvalidFactListHandler{
			Store:       query.NewPostgresAdminStore(db),
			Instruments: instruments,
		},
		// CloudInventory backs the list_cloud_resource_inventory MCP tool. It must
		// be mounted here (mirroring cmd/api/wiring.go) or the advertised tool
		// dispatches to /api/v0/cloud/inventory and 404s on the standalone MCP
		// server (#4071); the B-7 gate asserts this shape (#3866 criterion 4).
		CloudInventory: &query.CloudInventoryHandler{
			Content: contentReader,
			Profile: queryProfile,
		},
	}
}
