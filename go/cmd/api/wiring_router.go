// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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
	cookieSecureMode query.CookieSecureMode,
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
		cookieSecureMode,
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
	cookieSecureMode query.CookieSecureMode,
) (*query.APIRouter, error) {
	if statusReader == nil {
		statusReader = newStatusStore(pgstatus.SQLQueryer{DB: db}, instruments)
	}
	if governanceAudit == nil && db != nil {
		governanceAudit = newGovernanceAuditStore(db, instruments)
	}
	var containerImageIdentities query.ContainerImageIdentityStore
	var sbomAttachments query.SBOMAttestationAttachmentStore
	if db != nil {
		containerImageIdentities = query.NewPostgresContainerImageIdentityStore(db)
		sbomAttachments = query.NewPostgresSBOMAttestationAttachmentStore(db)
	}
	router := &query.APIRouter{
		LocalIdentity:          newLocalIdentityHandler(db, instruments, governanceAudit, cookieSecureMode),
		BrowserSessions:        newBrowserSessionHandler(db, instruments, cookieSecureMode),
		SessionList:            newBrowserSessionListHandler(db, instruments),
		AdminIdentityReads:     newAdminIdentityReadHandler(db, instruments, governanceAudit),
		AdminIdentityMutations: newAdminIdentityMutationHandler(db, instruments, governanceAudit),
		Profile:                newProfileHandler(db, instruments, governanceAudit),
		Repositories: &query.RepositoryHandler{
			Neo4j:                      neo4jReader,
			Content:                    contentReader,
			CICDRunCorrelations:        query.NewPostgresCICDRunCorrelationStore(db),
			ServiceCatalogCorrelations: query.NewPostgresServiceCatalogCorrelationStore(db),
			Freshness:                  pgstatus.NewInstrumentedRepositoryFreshnessStore(pgstatus.SQLQueryer{DB: db}, instruments),
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
			Instruments:              instruments,
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
			Neo4j:       neo4jReader,
			Aggregates:  query.NewGraphInfraResourceAggregateStore(neo4jReader),
			Profile:     queryProfile,
			Instruments: instruments,
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
			Inventory:    query.NewPostgresIaCInventoryStore(db),
			Graph:        neo4jReader,
			Profile:      queryProfile,
		},
		Impact: &query.ImpactHandler{
			Neo4j:       neo4jReader,
			Content:     contentReader,
			Profile:     queryProfile,
			Logger:      logger,
			Instruments: instruments,
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
		PackageRegistry: newPackageRegistryHandler(db, neo4jReader, contentReader, queryProfile),
		Dependencies: &query.DependenciesHandler{
			Neo4j:       neo4jReader,
			Profile:     queryProfile,
			Instruments: instruments,
		},
		CodeownersOwnership: &query.CodeownersOwnershipHandler{
			Neo4j:        neo4jReader,
			Correlations: query.NewPostgresServiceCatalogCorrelationStore(db),
			Profile:      queryProfile,
			Instruments:  instruments,
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
			LiveActivity:    pgstatus.NewInstrumentedLiveActivityStore(pgstatus.SQLQueryer{DB: db}, instruments),
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
			Store:       query.NewPostgresAdminStore(db),
			Audit:       adminRecoveryAuditAppender(governanceAudit),
			Instruments: instruments,
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

// v1PrefixAliasMiddleware rewrites /api/v1/* request paths to /api/v0/*
// before passing to next. It is applied ahead of auth middleware so
// scoped-token and browser-session route classification sees the v0 path.
// The request is cloned; method, headers, query parameters, and body are
// preserved.
func v1PrefixAliasMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			r2 := r.Clone(r.Context())
			u := *r.URL
			u.Path = "/api/v0/" + r.URL.Path[len("/api/v1/"):]
			r2.URL = &u
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// deprecationHeadersMiddleware adds Deprecation: true and Sunset: <date>
// headers to every response whose request path starts with /api/v0/.
// It is applied BEFORE the v1→v0 rewrite middleware so /api/v1/ requests
// never carry the headers. Headers are set before the next handler runs,
// so they appear on error responses too.
func deprecationHeadersMiddleware(next http.Handler, sunset string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v0/") {
			w.Header().Set("Deprecation", "true")
			w.Header().Set("Sunset", sunset)
		}
		next.ServeHTTP(w, r)
	})
}

// readSunsetDate returns the date for the Sunset response header, read from
// ESHU_API_V0_SUNSET_DATE. If set, the value is validated against RFC 1123
// (http.TimeFormat); a parse failure falls back to the default with a warning.
func readSunsetDate(getenv func(string) string) string {
	const defaultDate = "Thu, 01 Jul 2027 00:00:00 GMT"
	if v := getenv("ESHU_API_V0_SUNSET_DATE"); v != "" {
		if _, err := time.Parse(time.RFC1123, v); err == nil {
			return v
		}
		slog.Warn(
			"ESHU_API_V0_SUNSET_DATE parse failed, using default",
			"value", v,
			"default", defaultDate,
		)
	}
	return defaultDate
}
