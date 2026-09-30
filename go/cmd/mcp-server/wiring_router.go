// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	adminstore "github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/query/capability"
	"github.com/eshu-hq/eshu/go/internal/query/cicd"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/incident/store"
	"github.com/eshu-hq/eshu/go/internal/query/kubernetes"
	"github.com/eshu-hq/eshu/go/internal/query/observability/coverage"
	"github.com/eshu-hq/eshu/go/internal/query/package/registry"
	"github.com/eshu-hq/eshu/go/internal/query/secrets"
	"github.com/eshu-hq/eshu/go/internal/query/semanticsearch"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/alerts"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/terraform/drift"
	"github.com/eshu-hq/eshu/go/internal/query/workitem"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
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
	return newMCPQueryRouterWithSemanticEmbeddingWithReadStore(db, pgstatus.NewSQLReadStore(db), neo4jReader, contentReader, statusReader, queryProfile, graphBackend, logger, instruments, semanticSearchEmbedding, componentHome, componentPolicy, governanceStatus, governanceAudit, readImpactFromWinners)
}

// newMCPQueryRouterWithSemanticEmbeddingWithReadStore routes business reads to the guarded reader.
func newMCPQueryRouterWithSemanticEmbeddingWithReadStore(
	writer *sql.DB, reader db.ReadStore, neo4jReader query.GraphQuery, contentReader query.ContentStore,
	statusReader status.Reader, queryProfile query.QueryProfile, graphBackend query.GraphBackend,
	logger *slog.Logger, instruments *telemetry.Instruments, semanticSearchEmbedding searchembedruntime.Config,
	componentHome string, componentPolicy component.Policy, governanceStatus query.GovernanceStatusConfig,
	governanceAudit query.GovernanceAuditSummaryReader, readImpactFromWinners bool,
) *query.APIRouter {
	if statusReader == nil {
		statusReader = newStatusStore(reader, instruments)
	}
	auditRead := governanceAudit
	if reader != nil {
		auditRead = auditstore.NewGovernanceAuditReader(reader)
	}
	var containerImageIdentities query.ContainerImageIdentityStore
	var sbomAttachments query.SBOMAttestationAttachmentStore
	if reader != nil {
		containerImageIdentities = query.NewPostgresContainerImageIdentityStoreWithReadStore(reader)
		sbomAttachments = query.NewPostgresSBOMAttestationAttachmentStoreWithReadStore(reader)
	}
	return &query.APIRouter{
		Repositories: &query.RepositoryHandler{
			Neo4j:                      neo4jReader,
			Content:                    contentReader,
			CICDRunCorrelations:        cicd.NewPostgresRunCorrelationStoreWithReadStore(reader),
			ServiceCatalogCorrelations: service.NewPostgresServiceCatalogCorrelationStoreWithReadStore(reader),
			// Freshness backs get_repository_freshness (#5143). It must be
			// wired here (mirroring cmd/api/wiring_router.go) or the
			// advertised MCP tool 503s with "repository freshness reader
			// not configured" on the standalone MCP server, even though
			// GET /api/v0/repositories/{id}/freshness works on cmd/api --
			// the B-7 golden-corpus gate's MCP query-truth phase asserts
			// this tool live against this binary.
			Freshness: pgstatus.NewInstrumentedRepositoryFreshnessStore(reader, instruments),
			Profile:   queryProfile,
		},
		Entities: &query.EntityHandler{
			GraphBackend:             graphBackend,
			Neo4j:                    neo4jReader,
			Content:                  contentReader,
			CICDRunCorrelations:      cicd.NewPostgresRunCorrelationStoreWithReadStore(reader),
			ContainerImageIdentities: containerImageIdentities,
			SBOMAttachments:          sbomAttachments,
			Profile:                  queryProfile,
			Logger:                   logger,
			Instruments:              instruments,
			ContentRelationships:     query.ContentIndexRelationshipBuilder{},
		},
		Code: &query.CodeHandler{
			GraphBackend:         graphBackend,
			Neo4j:                neo4jReader,
			Content:              contentReader,
			CodeFlow:             codemodel.NewPostgresCodeFlowStoreWithReadStore(reader),
			Profile:              queryProfile,
			HybridRanker:         newCodeHybridRanker(semanticSearchEmbedding),
			Logger:               logger,
			ContentRelationships: query.ContentIndexRelationshipBuilder{},
		},
		Language: &query.LanguageQueryHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
			Logger:  logger,
		},
		Content: &query.ContentHandler{
			Content:      contentReader,
			Profile:      queryProfile,
			HybridRanker: newContentHybridRanker(semanticSearchEmbedding),
		},
		Infra: &query.InfraHandler{
			GraphBackend:   graphBackend,
			Neo4j:          neo4jReader,
			Aggregates:     query.NewInfraResourceAggregateStoreWithReadStore(neo4jReader, reader, instruments),
			CloudResources: query.NewPostgresCloudResourceListStoreWithReadStore(reader),
			Profile:        queryProfile,
			Instruments:    instruments,
		},
		IaC: newMCPQueryIaCHandlerWithReadStore(reader, contentReader, neo4jReader, queryProfile),
		Impact: &query.ImpactHandler{
			Neo4j:                  neo4jReader,
			Content:                contentReader,
			Profile:                queryProfile,
			Logger:                 logger,
			Instruments:            instruments,
			KubernetesPodTemplates: deployment.NewPostgresKubernetesPodTemplateStoreWithReadStore(reader),
		},
		Evidence: &query.EvidenceHandler{
			Content:            contentReader,
			AdmissionDecisions: query.NewPostgresAdmissionDecisionReadStoreWithReadStore(reader),
			Profile:            queryProfile,
			StatusReader:       statusReader,
			Neo4j:              neo4jReader,
		},
		Documentation: &query.DocumentationHandler{
			Content:    contentReader,
			Aggregates: query.NewPostgresDocumentationFindingAggregateStoreWithReadStore(reader),
			Profile:    queryProfile,
		},
		SemanticEvidence: &query.SemanticEvidenceHandler{
			Content: contentReader,
			Profile: queryProfile,
		},
		SemanticSearch: &query.SemanticSearchHandler{
			Index:         semanticsearch.NewPostgresSemanticSearchIndexStoreWithReadStore(reader),
			LocalHybrid:   newSemanticSearchHybridWithReadStore(reader, semanticSearchEmbedding, instruments),
			ScopeResolver: newInstrumentedSemanticSearchScopeResolverWithReadStore(reader, instruments),
			Profile:       queryProfile,
			SearchVectorReady: semanticsearch.NewPostgresSearchVectorReadyStoreWithReadStore(reader, query.SearchVectorBuildIdentity{
				ProviderProfileID:  semanticSearchEmbedding.ProviderProfileID,
				SourceClass:        semanticSearchEmbedding.SourceClass,
				EmbeddingModelID:   semanticSearchEmbedding.EmbeddingModelID,
				VectorIndexVersion: semanticSearchEmbedding.VectorIndexVersion,
			}),
		},
		PackageRegistry: &query.PackageRegistryHandler{
			Neo4j:              neo4jReader,
			Content:            contentReader,
			Correlations:       registry.NewPostgresCorrelationStoreWithReadStore(reader),
			Aggregates:         query.NewGraphPackageRegistryAggregateStore(neo4jReader),
			CollectorReadiness: query.NewPostgresCollectorListReadinessStoreWithReadStore(reader),
			Profile:            queryProfile,
		},
		// CodeownersOwnership backs the list_codeowners_ownership MCP tool
		// (issue #5419 Phase 4c). It must be wired here (mirroring
		// cmd/api/wiring_router.go) or the advertised tool re-dispatches into a
		// nil handler on the standalone MCP server.
		CodeownersOwnership: &query.CodeownersOwnershipHandler{
			Neo4j:        neo4jReader,
			Correlations: service.NewPostgresServiceCatalogCorrelationStoreWithReadStore(reader),
			Profile:      queryProfile,
			Instruments:  instruments,
		},
		CICD: &query.CICDHandler{
			Content:            contentReader,
			Correlations:       cicd.NewPostgresRunCorrelationStoreWithReadStore(reader),
			Aggregates:         cicd.NewPostgresRunCorrelationAggregateStoreWithReadStore(reader),
			CollectorReadiness: query.NewPostgresCollectorListReadinessStoreWithReadStore(reader),
			Profile:            queryProfile,
		},
		ServiceCatalog: &query.ServiceCatalogHandler{
			Content:      contentReader,
			Correlations: service.NewPostgresServiceCatalogCorrelationStoreWithReadStore(reader),
			Profile:      queryProfile,
		},
		Kubernetes: &query.KubernetesHandler{
			Correlations: kubernetes.NewPostgresCorrelationStoreWithReadStore(reader),
			Profile:      queryProfile,
		},
		SecretsIAM: &query.SecretsIAMHandler{
			IdentityTrustChains:          secrets.NewPostgresIAMIdentityTrustChainStoreWithReadStore(reader),
			PrivilegePostureObservations: secrets.NewPostgresIAMPrivilegePostureObservationStoreWithReadStore(reader),
			SecretAccessPaths:            secrets.NewPostgresIAMSecretAccessPathStoreWithReadStore(reader),
			PostureGaps:                  secrets.NewPostgresIAMPostureGapStoreWithReadStore(reader),
			Summary:                      secrets.NewPostgresIAMPostureSummaryStoreWithReadStore(reader),
			GrantPosture:                 query.NewGraphSecretsIAMGrantPostureStore(neo4jReader),
			Profile:                      queryProfile,
		},
		ObservabilityCoverage: &query.ObservabilityCoverageHandler{
			Content:      contentReader,
			Correlations: coverage.NewPostgresCorrelationStoreWithReadStore(reader),
			Profile:      queryProfile,
		},
		CloudRuntimeDrift: &query.CloudRuntimeDriftHandler{
			Store:   query.NewPostgresMultiCloudRuntimeDriftStoreWithReadStore(reader),
			Profile: queryProfile,
		},
		TerraformConfigStateDrift: &query.TerraformConfigStateDriftHandler{
			Store:   drift.NewPostgresFindingStoreWithReadStore(reader),
			Profile: queryProfile,
		},
		SupplyChain: &query.SupplyChainHandler{
			Neo4j:                       neo4jReader,
			Logger:                      logger,
			Content:                     contentReader,
			SBOMAttachments:             query.NewPostgresSBOMAttestationAttachmentStoreWithReadStore(reader),
			SBOMAttachmentAggregates:    query.NewPostgresSBOMAttestationAttachmentAggregateStoreWithReadStore(reader),
			AdvisoryEvidence:            advisory.NewPostgresEvidenceStoreWithReadStore(reader),
			AdvisoryCatalog:             advisory.NewPostgresCatalogStoreWithReadStore(reader),
			ImpactFindings:              impact.NewPostgresFindingStoreWithReadStore(reader, readImpactFromWinners),
			ImpactAggregates:            impact.NewPostgresAggregateStoreWithReadStore(reader),
			ImpactExplanations:          impact.NewPostgresFindingStoreWithReadStore(reader, false),
			ContainerImageIdentities:    query.NewPostgresContainerImageIdentityStoreWithReadStore(reader),
			ContainerImageAggregates:    query.NewPostgresContainerImageIdentityAggregateStoreWithReadStore(reader),
			SecurityAlerts:              alerts.NewPostgresStoreWithReadStore(reader),
			SecurityAlertAggregates:     alerts.NewPostgresAggregateStoreWithReadStore(reader),
			Readiness:                   impact.NewPostgresReadinessStoreWithReadStore(reader),
			CloudResourceInventory:      query.NewPostgresCloudResourceListStoreWithReadStore(reader),
			KubernetesWorkloadInventory: kubernetes.NewPostgresRuntimeWorkloadStoreWithReadStore(reader),
			CollectorReadiness:          query.NewPostgresCollectorListReadinessStoreWithReadStore(reader),
			PacketResponder:             query.NewSupplyChainImpactPacketResponder(),
			Profile:                     queryProfile,
		},
		Incident: &query.IncidentHandler{
			Context: store.NewStoreWithReadStore(reader),
			// Authorizer gates scoped-token reads of get_incident_context (#2144:
			// "Authorize ... the get_incident_context MCP tool ... for scoped
			// tokens"). It was wired only in cmd/api's newIncidentHandler
			// (wiring_handlers.go), so a scoped token calling get_incident_context
			// on the standalone MCP server always got a fail-closed not-found --
			// found by the #5148 dual-main reflective completeness test
			// (TestNewMCPQueryRouterWiresEveryFieldOrDocumentsWhyNot below), which
			// flags any nil interface field inside a wired handler.
			Authorizer: store.NewPostgresIncidentRepositoryAuthorizerWithReadStore(reader),
			Profile:    queryProfile,
		},
		WorkItems: &query.WorkItemHandler{
			Evidence: workitem.NewPostgresEvidenceStoreWithReadStore(reader),
			Profile:  queryProfile,
		},
		Visualization: &query.VisualizationHandler{},
		Status: &query.StatusHandler{
			Neo4j:           neo4jReader,
			DB:              writer,
			StatusReader:    statusReader,
			GovernanceAudit: auditRead,
			Profile:         queryProfile,
			Governance:      governanceStatus,
		},
		ComponentExtensions: &query.ComponentExtensionsHandler{
			ComponentHome: componentHome,
			Policy:        componentPolicy,
			Profile:       queryProfile,
		},
		Freshness: &query.FreshnessHandler{
			Generations:         pgstatus.NewStatusStore(reader),
			ChangedSince:        pgstatus.NewStatusStore(reader),
			ServiceChangedSince: pgstatus.NewStatusStore(reader),
			Profile:             queryProfile,
		},
		ExtractionReadiness:    &query.CollectorExtractionReadinessHandler{Profile: queryProfile},
		FactSchemaVersions:     &query.FactSchemaVersionHandler{Profile: queryProfile},
		Playbooks:              &query.QueryPlaybookHandler{Profile: queryProfile},
		InvestigationWorkflows: &query.InvestigationWorkflowHandler{Profile: queryProfile},
		Capabilities:           &capability.Handler{Profile: queryProfile},
		SurfaceInventory:       &query.SurfaceInventoryHandler{Profile: queryProfile},
		Compare: &query.CompareHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
		},
		AdminDeadLetters: &query.AdminDeadLetterListHandler{
			Store: adminstore.NewReadStore(reader),
		},
		AdminInputInvalidFacts: &query.AdminInputInvalidFactListHandler{
			Store:       adminstore.NewReadStore(reader),
			Instruments: instruments,
		},
		AdminChangedSincePoisonedLinks: &query.AdminChangedSincePoisonedLinksHandler{
			Store:       adminstore.NewReadStore(reader),
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
		// TagHistory backs the list_container_image_tag_history MCP tool
		// (#5459). It must be wired here (mirroring cmd/api/wiring_router.go)
		// or the advertised tool re-dispatches into a nil handler on the
		// standalone MCP server.
		TagHistory: &query.TagHistoryHandler{
			Neo4j:   neo4jReader,
			Profile: queryProfile,
		},
	}
}
