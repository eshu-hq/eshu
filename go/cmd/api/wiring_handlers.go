// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/cicd"
	"github.com/eshu-hq/eshu/go/internal/query/incident/store"
	"github.com/eshu-hq/eshu/go/internal/query/kubernetes"
	"github.com/eshu-hq/eshu/go/internal/query/observability/coverage"
	"github.com/eshu-hq/eshu/go/internal/query/package/registry"
	"github.com/eshu-hq/eshu/go/internal/query/secrets"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/alerts"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/workitem"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// newIncidentEvidenceSourceWithReadStore builds the durable incident evidence source for the
// service intelligence report's incidents_support section: a catalog-service-id
// resolver plus an incident evidence loader, both over the shared Postgres query
// surface. The logger surfaces ambiguous-catalog and load failures to operators.
func newIncidentEvidenceSourceWithReadStore(reader db.Queryer, logger *slog.Logger) serviceintelhttp.IncidentEvidenceSource {
	return serviceintelhttp.NewDurableIncidentEvidenceSource(
		pgstatus.NewServiceCatalogIDResolver(reader),
		pgstatus.NewServiceIncidentEvidenceLoader(reader),
		logger,
	)
}

// newSupplyChainEvidenceSource builds the durable supply-chain evidence source
// for the service intelligence report's supply_chain section over the shared
// Postgres aggregate read model. The logger surfaces load failures to operators.
func newSupplyChainEvidenceSource(writer *sql.DB, logger *slog.Logger) serviceintelhttp.SupplyChainEvidenceSource {
	return newSupplyChainEvidenceSourceWithReadStore(pgstatus.NewSQLReadStore(writer), logger)
}

func newSupplyChainEvidenceSourceWithReadStore(reader db.ReadStore, logger *slog.Logger) serviceintelhttp.SupplyChainEvidenceSource {
	return serviceintelhttp.NewDurableSupplyChainEvidenceSource(
		impact.NewPostgresAggregateStoreWithReadStore(reader),
		logger,
	)
}

// newSupplyChainHandler builds the supply-chain read handler with its full set
// of Postgres-backed evidence, advisory, impact, container-image, and security
// alert stores. Extracted from newRouter to keep wiring.go cohesive; the field
// wiring is identical to the inline construction it replaced.
func newSupplyChainHandler(
	writer *sql.DB,
	reader db.ReadStore,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	profile query.QueryProfile,
	readImpactFromWinners bool,
	logger *slog.Logger,
) *query.SupplyChainHandler {
	return &query.SupplyChainHandler{
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
		SuppressionMutations:        query.NewPostgresVulnerabilitySuppressionMutationStore(writer),
		CloudResourceInventory:      query.NewPostgresCloudResourceListStoreWithReadStore(reader),
		KubernetesWorkloadInventory: kubernetes.NewPostgresRuntimeWorkloadStoreWithReadStore(reader),
		CollectorReadiness:          query.NewPostgresCollectorListReadinessStoreWithReadStore(reader),
		PacketResponder:             query.NewSupplyChainImpactPacketResponder(),
		Profile:                     profile,
	}
}

// newSecretsIAMHandler builds the secrets/IAM posture read handler over its
// Postgres trust-chain, privilege-posture, access-path, gap, and summary
// stores plus the graph-backed S3 external-principal grant posture reader
// (issue #5643).
func newSecretsIAMHandler(
	reader db.ReadStore,
	neo4jReader query.GraphQuery,
	profile query.QueryProfile,
) *query.SecretsIAMHandler {
	return &query.SecretsIAMHandler{
		IdentityTrustChains:          secrets.NewPostgresIAMIdentityTrustChainStoreWithReadStore(reader),
		PrivilegePostureObservations: secrets.NewPostgresIAMPrivilegePostureObservationStoreWithReadStore(reader),
		SecretAccessPaths:            secrets.NewPostgresIAMSecretAccessPathStoreWithReadStore(reader),
		PostureGaps:                  secrets.NewPostgresIAMPostureGapStoreWithReadStore(reader),
		Summary:                      secrets.NewPostgresIAMPostureSummaryStoreWithReadStore(reader),
		GrantPosture:                 query.NewGraphSecretsIAMGrantPostureStore(neo4jReader),
		Profile:                      profile,
	}
}

// newPackageRegistryHandler builds the package-registry read handler over the
// graph reader, content store, and Postgres correlation/aggregate stores.
func newPackageRegistryHandler(
	reader db.ReadStore,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	profile query.QueryProfile,
) *query.PackageRegistryHandler {
	return &query.PackageRegistryHandler{
		Neo4j:              neo4jReader,
		Content:            contentReader,
		Correlations:       registry.NewPostgresCorrelationStoreWithReadStore(reader),
		Aggregates:         query.NewGraphPackageRegistryAggregateStore(neo4jReader),
		CollectorReadiness: query.NewPostgresCollectorListReadinessStoreWithReadStore(reader),
		Profile:            profile,
	}
}

// newCICDHandler builds the CI/CD read handler over the content store and the
// Postgres run-correlation and aggregate stores.
func newCICDHandler(reader db.ReadStore, contentReader query.ContentStore, profile query.QueryProfile) *query.CICDHandler {
	return &query.CICDHandler{
		Content:            contentReader,
		Correlations:       cicd.NewPostgresRunCorrelationStoreWithReadStore(reader),
		Aggregates:         cicd.NewPostgresRunCorrelationAggregateStoreWithReadStore(reader),
		CollectorReadiness: query.NewPostgresCollectorListReadinessStoreWithReadStore(reader),
		Profile:            profile,
	}
}

// newServiceCatalogHandler builds the service-catalog read handler over the
// content store and Postgres service-catalog correlation store.
func newServiceCatalogHandler(reader db.ReadStore, contentReader query.ContentStore, profile query.QueryProfile) *query.ServiceCatalogHandler {
	return &query.ServiceCatalogHandler{
		Content:      contentReader,
		Correlations: service.NewPostgresServiceCatalogCorrelationStoreWithReadStore(reader),
		Profile:      profile,
	}
}

// newKubernetesHandler builds the Kubernetes read handler over the Postgres
// Kubernetes correlation store.
func newKubernetesHandler(reader db.ReadStore, profile query.QueryProfile) *query.KubernetesHandler {
	return &query.KubernetesHandler{
		Correlations: kubernetes.NewPostgresCorrelationStoreWithReadStore(reader),
		Profile:      profile,
	}
}

// newObservabilityCoverageHandler builds the observability-coverage read handler
// over the content store and Postgres coverage correlation store.
func newObservabilityCoverageHandler(reader db.ReadStore, contentReader query.ContentStore, profile query.QueryProfile) *query.ObservabilityCoverageHandler {
	return &query.ObservabilityCoverageHandler{
		Content:      contentReader,
		Correlations: coverage.NewPostgresCorrelationStoreWithReadStore(reader),
		Profile:      profile,
	}
}

// newIncidentHandler builds the incident read handler over the Postgres incident
// context store and the repository authorizer that gates incident access.
func newIncidentHandler(reader db.ReadStore, profile query.QueryProfile) *query.IncidentHandler {
	return &query.IncidentHandler{
		Context:    store.NewStoreWithReadStore(reader),
		Authorizer: store.NewPostgresIncidentRepositoryAuthorizerWithReadStore(reader),
		Profile:    profile,
	}
}

// newWorkItemHandler builds the work-item read handler over the Postgres
// work-item evidence store.
func newWorkItemHandler(reader db.ReadStore, profile query.QueryProfile) *query.WorkItemHandler {
	return &query.WorkItemHandler{
		Evidence: workitem.NewPostgresEvidenceStoreWithReadStore(reader),
		Profile:  profile,
	}
}

// newFreshnessHandler builds the freshness read handler. The generation,
// changed-since, and service-changed-since readers are all backed by the
// Postgres status store, and the service-ownership resolver by the
// service-catalog correlation read model; all remain nil when db is nil,
// matching the prior inline behavior in newRouter.
func newFreshnessHandler(reader db.ReadStore, profile query.QueryProfile) *query.FreshnessHandler {
	var (
		generationLifecycle query.GenerationLifecycleReader
		changedSince        query.ChangedSinceReader
		serviceChangedSince query.ServiceChangedSinceReader
	)
	if reader != nil {
		generationLifecycle = pgstatus.NewStatusStore(reader)
		changedSince = pgstatus.NewStatusStore(reader)
		serviceChangedSince = pgstatus.NewStatusStore(reader)
	}
	return &query.FreshnessHandler{
		Generations:         generationLifecycle,
		ChangedSince:        changedSince,
		ServiceChangedSince: serviceChangedSince,
		Profile:             profile,
	}
}
