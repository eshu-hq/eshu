// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/capability"
	"github.com/eshu-hq/eshu/go/internal/query/cicd"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/entity/content/fallback"
	"github.com/eshu-hq/eshu/go/internal/query/iac"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/incident/store"
	"github.com/eshu-hq/eshu/go/internal/query/kubernetes"
	"github.com/eshu-hq/eshu/go/internal/query/manifest"
	"github.com/eshu-hq/eshu/go/internal/query/observability/coverage"
	"github.com/eshu-hq/eshu/go/internal/query/package/registry"
	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/query/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/query/secrets"
	"github.com/eshu-hq/eshu/go/internal/query/semanticsearch"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/terraform/drift"
	"github.com/eshu-hq/eshu/go/internal/query/workitem"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// BuildCoreRouter wires every query handler from live stores and the
// binary-built auth wiring. The composition mirrors the retired cmd/api
// wiring (oracle 8a78eb3f48) re-derived in package query: same store
// implementations behind the same handler fields, no handler bodies carried
// over. Secret-sealed material never crosses this package (see
// secretcrypto_open_boundary_test.go): the binary builds the auth-family
// adapters in cmd/core-api and passes them through CoreAuthWiring.
// Nil-tolerant by construction: a nil DB leaves Postgres-backed fields nil
// (handlers fail closed 503 at request time) while every handler stays
// non-nil so the full served set mounts.
func BuildCoreRouter(deps CoreServeDeps) (*APIRouter, error) {
	stores, err := assembleCoreRouterStores(deps)
	if err != nil {
		return nil, err
	}
	router := newCoreReadRouterBase(deps, stores)
	completeCoreReadRouter(deps, stores, router)
	completeCoreStatusRouter(deps, stores, router)
	mountCoreAuthHandlers(router, deps.Auth)
	if err := wireCoreAdminRecovery(router, stores); err != nil {
		return nil, err
	}
	if err := startCoreInfraSearchKeyRead(deps.Getenv, router.Infra, deps.GraphBackend, deps.GraphDriven, stores.graph, stores.logger, stores.instruments); err != nil {
		return nil, err
	}
	if deps.NarrationPosture != nil && router.Status != nil {
		router.Status.NarrationPosture = deps.NarrationPosture
	}

	return router, nil
}

// coreRouterStores bundles the shared store handles BuildCoreRouter
// assembles once and threads through the per-group router builders.
type coreRouterStores struct {
	profile              QueryProfile
	logger               *slog.Logger
	instruments          *telemetry.Instruments
	writer               *sql.DB
	reader               db.ReadStore
	graph                GraphQuery
	content              ContentStore
	status               status.Reader
	metrics              MetricsTimeSeriesSource
	semantic             searchembedruntime.Config
	governanceAudit      GovernanceAuditSummaryReader
	manifestDependencies manifest.ManifestDependenciesReadAdmission
}

// assembleCoreRouterStores opens the shared read models: the Postgres
// reader, content reader, status snapshot reader, semantic embedder config,
// and governance audit store. The metrics source arrives prebuilt via deps
// (the binary owns store wiring); nil fails closed.
func assembleCoreRouterStores(deps CoreServeDeps) (*coreRouterStores, error) {
	profile := deps.Profile
	if profile == "" {
		profile = ProfileProduction
	}
	logger := coreLogger(deps.Logger)
	instruments := deps.Instruments
	var writer *sql.DB
	var reader db.ReadStore
	if deps.DB != nil {
		writer = deps.DB
		reader = pgstatus.NewSQLReadStore(writer)
	}
	var contentReader ContentStore
	if reader != nil {
		contentReader = NewContentReaderWithReadStore(reader).
			WithInstruments(instruments)
	}
	statusReader, err := newCoreStatusReader(reader, instruments, deps.Getenv)
	if err != nil {
		return nil, err
	}
	metricsSource := deps.Metrics
	semanticEmbedding, err := searchembedruntime.ConfigFromEnv(deps.Getenv, nil)
	if err != nil {
		return nil, err
	}
	return &coreRouterStores{
		profile:              profile,
		logger:               logger,
		instruments:          instruments,
		writer:               writer,
		reader:               reader,
		graph:                deps.Graph,
		content:              contentReader,
		status:               statusReader,
		metrics:              metricsSource,
		semantic:             semanticEmbedding,
		governanceAudit:      CoreGovernanceAuditStore(writer, instruments, logger),
		manifestDependencies: manifest.NewPostgresManifestDependencyStore(writer, instruments, logger),
	}, nil
}

// newCoreReadRouterBase mounts the first read-family group: repositories,
// code, content, infra, and semantic evidence.
func newCoreReadRouterBase(deps CoreServeDeps, stores *coreRouterStores) *APIRouter {
	profile := stores.profile
	logger := stores.logger
	instruments := stores.instruments
	reader := stores.reader
	graphReader := stores.graph
	contentReader := stores.content
	statusReader := stores.status
	semanticEmbedding := stores.semantic
	manifestDependencies := stores.manifestDependencies
	return &APIRouter{
		Repositories: &RepositoryHandler{
			Neo4j:                      graphReader,
			Content:                    contentReader,
			CICDRunCorrelations:        cicd.NewPostgresRunCorrelationStoreWithReadStore(reader),
			ServiceCatalogCorrelations: service.NewPostgresServiceCatalogCorrelationStoreWithReadStore(reader),
			Freshness:                  pgstatus.NewInstrumentedRepositoryFreshnessStore(reader, instruments),
			Selections:                 pgstatus.NewPageSelectionStore(reader),
			Retirements:                pgstatus.NewRetirementStore(reader),
			ManifestDependencies:       manifestDependencies,
			Profile:                    profile,
			Logger:                     logger,
			Instruments:                instruments,
		},
		Entities: &EntityHandler{
			GraphBackend:             deps.GraphBackend,
			Neo4j:                    graphReader,
			Content:                  contentReader,
			CICDRunCorrelations:      cicd.NewPostgresRunCorrelationStoreWithReadStore(reader),
			ContainerImageIdentities: NewPostgresContainerImageIdentityStoreWithReadStore(reader),
			SBOMAttachments:          NewPostgresSBOMAttestationAttachmentStoreWithReadStore(reader),
			Profile:                  profile,
			Logger:                   logger,
			Instruments:              instruments,
			ContentRelationships:     fallback.Builder{},
		},
		Code: &CodeHandler{
			GraphBackend:         deps.GraphBackend,
			Neo4j:                graphReader,
			Content:              contentReader,
			CodeFlow:             codemodel.NewPostgresCodeFlowStoreWithReadStore(reader),
			Profile:              profile,
			HybridRanker:         newCoreCodeHybridRanker(semanticEmbedding),
			Logger:               logger,
			ContentRelationships: fallback.Builder{},
		},
		Language: &LanguageQueryHandler{
			Neo4j:       graphReader,
			Content:     contentReader,
			Profile:     profile,
			Logger:      logger,
			Instruments: instruments,
		},
		Content: &ContentHandler{
			Content:      contentReader,
			Profile:      profile,
			HybridRanker: newCoreContentHybridRanker(semanticEmbedding),
		},
		Infra: &InfraHandler{
			GraphBackend:   deps.GraphBackend,
			Neo4j:          graphReader,
			Aggregates:     NewInfraResourceAggregateStoreWithReadStore(graphReader, reader, instruments),
			CloudResources: NewPostgresCloudResourceListStoreWithReadStore(reader),
			Profile:        profile,
			Instruments:    instruments,
		},
		CloudRuntimeDrift: &CloudRuntimeDriftHandler{
			Store:   NewPostgresMultiCloudRuntimeDriftStoreWithReadStore(reader),
			Profile: profile,
		},
		TerraformConfigStateDrift: &TerraformConfigStateDriftHandler{
			Store:   drift.NewPostgresFindingStoreWithReadStore(reader),
			Profile: profile,
		},
		IaC: &IaCHandler{
			Content:      contentReader,
			Reachability: iac.NewPostgresIaCReachabilityStoreWithReadStore(reader),
			Management:   iac.NewPostgresIaCManagementStoreWithReadStore(reader),
			Inventory:    iac.NewPostgresIaCInventoryStoreWithReadStore(reader),
			Graph:        graphReader,
			Profile:      profile,
		},
		Impact: &ImpactHandler{
			Neo4j:                  graphReader,
			Content:                contentReader,
			Profile:                profile,
			Logger:                 logger,
			Instruments:            instruments,
			KubernetesPodTemplates: deployment.NewPostgresKubernetesPodTemplateStoreWithReadStore(reader),
			ECSTasks:               deployment.NewPostgresECSTaskStore(reader),
			CodeSurface:            NewChangeSurfaceCodeBackend(),
			TraceContext:           NewDeploymentTraceContext(),
			PathProbe:              NewImpactPathProbeBackend(),
		},
		Evidence: &EvidenceHandler{
			Content:            contentReader,
			AdmissionDecisions: NewPostgresAdmissionDecisionReadStoreWithReadStore(reader),
			Profile:            profile,
			StatusReader:       statusReader,
			Neo4j:              graphReader,
			PacketResponder:    NewDeployableUnitPacketResponder(),
		},
		Documentation: &DocumentationHandler{
			Content:    contentReader,
			Aggregates: NewPostgresDocumentationFindingAggregateStoreWithReadStore(reader),
			Profile:    profile,
		},
		SemanticEvidence: &SemanticEvidenceHandler{
			Content: contentReader,
			Profile: profile,
		},
		SemanticSearch: &SemanticSearchHandler{
			Index:         semanticsearch.NewPostgresSemanticSearchIndexStoreWithReadStore(reader),
			LocalHybrid:   newCoreSemanticSearchHybrid(reader, semanticEmbedding, instruments),
			ScopeResolver: newCoreSemanticSearchScopeResolver(reader, instruments),
			Profile:       profile,
			SearchVectorReady: semanticsearch.NewPostgresSearchVectorReadyStoreWithReadStore(reader,
				SearchVectorBuildIdentity{
					ProviderProfileID:  semanticEmbedding.ProviderProfileID,
					SourceClass:        semanticEmbedding.SourceClass,
					EmbeddingModelID:   semanticEmbedding.EmbeddingModelID,
					VectorIndexVersion: semanticEmbedding.VectorIndexVersion,
				}),
		},
	}
}

// mountCoreAuthHandlers assigns the binary-built auth-family handlers,
// falling back to empty fail-closed handlers when the binary passes none
// (tests). Every route stays mounted in both postures.
func mountCoreAuthHandlers(router *APIRouter, auth *CoreAuthWiring) {
	if auth == nil {
		auth = &CoreAuthWiring{}
	}
	router.LocalIdentity = firstNonNilAuth(auth.LocalIdentity, &LocalIdentityHandler{})
	router.BrowserSessions = firstNonNilAuth(auth.BrowserSessions, &BrowserSessionHandler{})
	router.SessionList = firstNonNilAuth(auth.SessionList, &BrowserSessionListHandler{})
	router.AdminIdentityReads = firstNonNilAuth(auth.AdminIdentityReads, &AdminIdentityReadHandler{})
	router.AdminIdentityMutations = firstNonNilAuth(auth.AdminIdentityMutations, &AdminIdentityMutationHandler{})
	router.Profile = firstNonNilAuth(auth.Profile, &ProfileHandler{})
	router.SignInPolicyReads = firstNonNilAuth(auth.SignInPolicyReads, &SignInPolicyReadHandler{})
	router.SignInPolicyMutations = firstNonNilAuth(auth.SignInPolicyMutations, &SignInPolicyMutationHandler{})
	router.Setup = firstNonNilAuth(auth.Setup, &SetupHandler{})
	router.OIDCLogin = firstNonNilAuth(auth.OIDCLogin, &OIDCLoginHandler{})
	router.SAML = firstNonNilAuth(auth.SAML, &SAMLHandler{})
	router.GitHubLogin = firstNonNilAuth(auth.GitHubLogin, &GitHubLoginHandler{})
	router.AuthProviders = firstNonNilAuth(auth.AuthProviders, &AuthProviderListHandler{})
	router.AdminProviderConfigReads = firstNonNilAuth(auth.AdminProviderConfigReads, &AdminProviderConfigReadHandler{})
	router.AdminProviderConfigMutations = firstNonNilAuth(auth.AdminProviderConfigMutations, &AdminProviderConfigMutationHandler{})
}

// firstNonNilAuth returns the primary handler, or the empty fail-closed
// fallback when the binary passed none.
func firstNonNilAuth[T any](primary, fallback *T) *T {
	if primary != nil {
		return primary
	}
	return fallback
}

// completeCoreReadRouter mounts the second read-family group: package,
// dependency, service, supply chain, and admin read models.
func completeCoreReadRouter(deps CoreServeDeps, stores *coreRouterStores, router *APIRouter) {
	profile := stores.profile
	logger := stores.logger
	instruments := stores.instruments
	writer := stores.writer
	reader := stores.reader
	graphReader := stores.graph
	contentReader := stores.content
	manifestDependencies := stores.manifestDependencies
	router.GraphEntityInventory = &GraphEntityInventoryHandler{
		Neo4j:   graphReader,
		Profile: profile,
	}
	router.CloudInventory = &CloudInventoryHandler{
		Content: contentReader,
		Profile: profile,
	}
	router.SSMHostInventory = &SSMHostInventoryHandler{
		Content: contentReader,
		Graph:   graphReader,
		Profile: profile,
		Bounds:  loadCoreSSMHostBounds(deps.Getenv),
		Now:     time.Now,
	}

	router.PackageRegistry = &PackageRegistryHandler{
		Neo4j:              graphReader,
		Content:            contentReader,
		Correlations:       registry.NewPostgresCorrelationStoreWithReadStore(reader),
		Aggregates:         NewGraphPackageRegistryAggregateStore(graphReader),
		CollectorReadiness: NewPostgresCollectorListReadinessStoreWithReadStore(reader),
		Profile:            profile,
	}
	router.Dependencies = &DependenciesHandler{
		Neo4j:       graphReader,
		Profile:     profile,
		Instruments: instruments,
	}
	router.ManifestDependencies = &manifest.Handler{
		Store:       manifestDependencies,
		Profile:     profile,
		Instruments: instruments,
	}
	router.CodeownersOwnership = &CodeownersOwnershipHandler{
		Neo4j:        graphReader,
		Correlations: service.NewPostgresServiceCatalogCorrelationStoreWithReadStore(reader),
		Profile:      profile,
		Instruments:  instruments,
	}
	router.RuntimeDeclarations = &runtimedeclaration.Handler{
		Neo4j:       graphReader,
		Profile:     profile,
		Instruments: instruments,
	}
	router.CICD = &CICDHandler{
		Content:            contentReader,
		Correlations:       cicd.NewPostgresRunCorrelationStoreWithReadStore(reader),
		Aggregates:         cicd.NewPostgresRunCorrelationAggregateStoreWithReadStore(reader),
		CollectorReadiness: NewPostgresCollectorListReadinessStoreWithReadStore(reader),
		Profile:            profile,
	}
	router.ServiceCatalog = &ServiceCatalogHandler{
		Content:      contentReader,
		Correlations: service.NewPostgresServiceCatalogCorrelationStoreWithReadStore(reader),
		Profile:      profile,
	}
	router.Kubernetes = &KubernetesHandler{
		Correlations: kubernetes.NewPostgresCorrelationStoreWithReadStore(reader),
		Profile:      profile,
	}
	router.SecretsIAM = &SecretsIAMHandler{
		IdentityTrustChains:          secrets.NewPostgresIAMIdentityTrustChainStoreWithReadStore(reader),
		PrivilegePostureObservations: secrets.NewPostgresIAMPrivilegePostureObservationStoreWithReadStore(reader),
		SecretAccessPaths:            secrets.NewPostgresIAMSecretAccessPathStoreWithReadStore(reader),
		PostureGaps:                  secrets.NewPostgresIAMPostureGapStoreWithReadStore(reader),
		Summary:                      secrets.NewPostgresIAMPostureSummaryStoreWithReadStore(reader),
		GrantPosture:                 NewGraphSecretsIAMGrantPostureStore(graphReader),
		Profile:                      profile,
	}
	router.ObservabilityCoverage = &ObservabilityCoverageHandler{
		Content:      contentReader,
		Correlations: coverage.NewPostgresCorrelationStoreWithReadStore(reader),
		Profile:      profile,
	}
	router.Images = &ImageHandler{
		Neo4j:   graphReader,
		Profile: profile,
	}
	router.TagHistory = &TagHistoryHandler{
		Neo4j:   graphReader,
		Profile: profile,
	}
	router.SupplyChain = newCoreSupplyChainHandler(
		NewPostgresVulnerabilitySuppressionMutationStore(writer),
		reader, graphReader, contentReader, profile,
		SupplyChainImpactWinnersReadEnabled(coreEnvOrEmpty(deps.Getenv, SupplyChainImpactWinnersReadEnv)),
		logger, instruments,
	)
	router.Incident = &IncidentHandler{
		Context:    store.NewStoreWithReadStore(reader),
		Authorizer: store.NewPostgresIncidentRepositoryAuthorizerWithReadStore(reader),
		Profile:    profile,
	}
	router.WorkItems = &WorkItemHandler{
		Evidence: workitem.NewPostgresEvidenceStoreWithReadStore(reader),
		Profile:  profile,
	}
	router.Visualization = &VisualizationHandler{}
	router.Freshness = newCoreFreshnessHandler(reader, profile, deps.Getenv)
}

// completeCoreStatusRouter mounts the status surface, admin surface, and
// ask handler: the last mount group.
func completeCoreStatusRouter(deps CoreServeDeps, stores *coreRouterStores, router *APIRouter) {
	profile := stores.profile
	instruments := stores.instruments
	writer := stores.writer
	reader := stores.reader
	graphReader := stores.graph
	contentReader := stores.content
	statusReader := stores.status
	metricsSource := stores.metrics
	governanceAudit := stores.governanceAudit
	router.Status = &StatusHandler{
		Neo4j:           graphReader,
		GraphBackend:    deps.GraphBackend,
		DB:              writer,
		StatusReader:    statusReader,
		Retirements:     pgstatus.NewRetirementStore(reader),
		GovernanceAudit: governanceAudit,
		Profile:         profile,
		Governance:      deps.Governance,
		LiveActivity:    pgstatus.NewInstrumentedLiveActivityStore(reader, instruments),
	}
	router.ComponentExtensions = &ComponentExtensionsHandler{
		ComponentHome: deps.ComponentHome,
		Policy:        deps.ComponentPolicy,
		Profile:       profile,
	}
	router.ExtractionReadiness = &CollectorExtractionReadinessHandler{Profile: profile}
	router.FactSchemaVersions = &FactSchemaVersionHandler{Profile: profile}
	router.Playbooks = &QueryPlaybookHandler{Profile: profile}
	router.InvestigationWorkflows = &InvestigationWorkflowHandler{Profile: profile}
	router.Metrics = &MetricsHandler{
		Source:  metricsSource,
		Profile: profile,
	}
	router.Capabilities = &capability.Handler{Profile: profile}
	router.SurfaceInventory = &SurfaceInventoryHandler{Profile: profile}
	router.Compare = &CompareHandler{
		Neo4j:   graphReader,
		Content: contentReader,
		Profile: profile,
	}
	router.Admin = &AdminHandler{
		Store:       NewPostgresAdminStore(writer, instruments),
		ReadStore:   newCoreAdminReadStore(reader),
		Audit:       coreRecoveryAuditAppender(governanceAudit),
		Instruments: instruments,
		Completeness: &admin.DeltaUnionCompletenessChecker{
			Active: pgstatus.NewFactStore(pgstatus.SQLDB{DB: writer}),
			Graph:  graphReader,
		},
	}
}
