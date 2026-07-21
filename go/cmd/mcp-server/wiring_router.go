// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newMCPQueryRouter and newMCPQueryRouterWithSemanticEmbedding build the
// full query.APIRouter wiring for the standalone MCP server, mirroring
// cmd/api/wiring_router.go. Split out of wiring.go (which owns wireAPI, the
// process-level bootstrap) to keep both files under the repo's 500-line
// package-file cap.

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
			Neo4j:          neo4jReader,
			Aggregates:     query.NewGraphInfraResourceAggregateStore(neo4jReader),
			CloudResources: query.NewPostgresCloudResourceListStore(db),
			Profile:        queryProfile,
			Instruments:    instruments,
		},
		IaC: newMCPQueryIaCHandler(db, contentReader, neo4jReader, queryProfile),
		Impact: &query.ImpactHandler{
			Neo4j:                  neo4jReader,
			Content:                contentReader,
			Profile:                queryProfile,
			Logger:                 logger,
			Instruments:            instruments,
			KubernetesPodTemplates: query.NewPostgresKubernetesPodTemplateStore(db),
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
		// CodeownersOwnership backs the list_codeowners_ownership MCP tool
		// (issue #5419 Phase 4c). It must be wired here (mirroring
		// cmd/api/wiring_router.go) or the advertised tool re-dispatches into a
		// nil handler on the standalone MCP server.
		CodeownersOwnership: &query.CodeownersOwnershipHandler{
			Neo4j:        neo4jReader,
			Correlations: query.NewPostgresServiceCatalogCorrelationStore(db),
			Profile:      queryProfile,
			Instruments:  instruments,
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
		TerraformConfigStateDrift: &query.TerraformConfigStateDriftHandler{
			Store:   query.NewPostgresTerraformConfigStateDriftFindingStore(db),
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
