// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// newIncidentEvidenceSource builds the durable incident evidence source for the
// service intelligence report's incidents_support section: a catalog-service-id
// resolver plus an incident evidence loader, both over the shared Postgres query
// surface. The logger surfaces ambiguous-catalog and load failures to operators.
func newIncidentEvidenceSource(db *sql.DB, logger *slog.Logger) serviceintelhttp.IncidentEvidenceSource {
	queryer := pgstatus.SQLQueryer{DB: db}
	return serviceintelhttp.NewDurableIncidentEvidenceSource(
		pgstatus.NewServiceCatalogIDResolver(queryer),
		pgstatus.NewServiceIncidentEvidenceLoader(queryer),
		logger,
	)
}

// newSupplyChainEvidenceSource builds the durable supply-chain evidence source
// for the service intelligence report's supply_chain section over the shared
// Postgres aggregate read model. The logger surfaces load failures to operators.
func newSupplyChainEvidenceSource(db *sql.DB, logger *slog.Logger) serviceintelhttp.SupplyChainEvidenceSource {
	return serviceintelhttp.NewDurableSupplyChainEvidenceSource(
		query.NewPostgresSupplyChainImpactAggregateStore(db),
		logger,
	)
}

// newSupplyChainHandler builds the supply-chain read handler with its full set
// of Postgres-backed evidence, advisory, impact, container-image, and security
// alert stores. Extracted from newRouter to keep wiring.go cohesive; the field
// wiring is identical to the inline construction it replaced.
func newSupplyChainHandler(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	profile query.QueryProfile,
	readImpactFromWinners bool,
) *query.SupplyChainHandler {
	return &query.SupplyChainHandler{
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
		Profile:                  profile,
	}
}

// newSecretsIAMHandler builds the secrets/IAM posture read handler over its
// Postgres trust-chain, privilege-posture, access-path, gap, and summary
// stores plus the graph-backed S3 external-principal grant posture reader
// (issue #5643).
func newSecretsIAMHandler(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	profile query.QueryProfile,
) *query.SecretsIAMHandler {
	return &query.SecretsIAMHandler{
		IdentityTrustChains:          query.NewPostgresSecretsIAMIdentityTrustChainStore(db),
		PrivilegePostureObservations: query.NewPostgresSecretsIAMPrivilegePostureObservationStore(db),
		SecretAccessPaths:            query.NewPostgresSecretsIAMSecretAccessPathStore(db),
		PostureGaps:                  query.NewPostgresSecretsIAMPostureGapStore(db),
		Summary:                      query.NewPostgresSecretsIAMPostureSummaryStore(db),
		GrantPosture:                 query.NewGraphSecretsIAMGrantPostureStore(neo4jReader),
		Profile:                      profile,
	}
}

// newPackageRegistryHandler builds the package-registry read handler over the
// graph reader, content store, and Postgres correlation/aggregate stores.
func newPackageRegistryHandler(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	profile query.QueryProfile,
) *query.PackageRegistryHandler {
	return &query.PackageRegistryHandler{
		Neo4j:              neo4jReader,
		Content:            contentReader,
		Correlations:       query.NewPostgresPackageRegistryCorrelationStore(db),
		Aggregates:         query.NewGraphPackageRegistryAggregateStore(neo4jReader),
		CollectorReadiness: query.NewPostgresCollectorListReadinessStore(db),
		Profile:            profile,
	}
}

// newCICDHandler builds the CI/CD read handler over the content store and the
// Postgres run-correlation and aggregate stores.
func newCICDHandler(db *sql.DB, contentReader query.ContentStore, profile query.QueryProfile) *query.CICDHandler {
	return &query.CICDHandler{
		Content:            contentReader,
		Correlations:       query.NewPostgresCICDRunCorrelationStore(db),
		Aggregates:         query.NewPostgresCICDRunCorrelationAggregateStore(db),
		CollectorReadiness: query.NewPostgresCollectorListReadinessStore(db),
		Profile:            profile,
	}
}

// newServiceCatalogHandler builds the service-catalog read handler over the
// content store and Postgres service-catalog correlation store.
func newServiceCatalogHandler(db *sql.DB, contentReader query.ContentStore, profile query.QueryProfile) *query.ServiceCatalogHandler {
	return &query.ServiceCatalogHandler{
		Content:      contentReader,
		Correlations: query.NewPostgresServiceCatalogCorrelationStore(db),
		Profile:      profile,
	}
}

// newKubernetesHandler builds the Kubernetes read handler over the Postgres
// Kubernetes correlation store.
func newKubernetesHandler(db *sql.DB, profile query.QueryProfile) *query.KubernetesHandler {
	return &query.KubernetesHandler{
		Correlations: query.NewPostgresKubernetesCorrelationStore(db),
		Profile:      profile,
	}
}

// newObservabilityCoverageHandler builds the observability-coverage read handler
// over the content store and Postgres coverage correlation store.
func newObservabilityCoverageHandler(db *sql.DB, contentReader query.ContentStore, profile query.QueryProfile) *query.ObservabilityCoverageHandler {
	return &query.ObservabilityCoverageHandler{
		Content:      contentReader,
		Correlations: query.NewPostgresObservabilityCoverageCorrelationStore(db),
		Profile:      profile,
	}
}

// newIncidentHandler builds the incident read handler over the Postgres incident
// context store and the repository authorizer that gates incident access.
func newIncidentHandler(db *sql.DB, profile query.QueryProfile) *query.IncidentHandler {
	return &query.IncidentHandler{
		Context:    query.NewPostgresIncidentContextStore(db),
		Authorizer: query.NewPostgresIncidentRepositoryAuthorizer(db),
		Profile:    profile,
	}
}

// newWorkItemHandler builds the work-item read handler over the Postgres
// work-item evidence store.
func newWorkItemHandler(db *sql.DB, profile query.QueryProfile) *query.WorkItemHandler {
	return &query.WorkItemHandler{
		Evidence: query.NewPostgresWorkItemEvidenceStore(db),
		Profile:  profile,
	}
}

// newFreshnessHandler builds the freshness read handler. The generation,
// changed-since, and service-changed-since readers are all backed by the
// Postgres status store and remain nil when db is nil, matching the prior
// inline behavior in newRouter.
func newFreshnessHandler(db *sql.DB, profile query.QueryProfile) *query.FreshnessHandler {
	var (
		generationLifecycle query.GenerationLifecycleReader
		changedSince        query.ChangedSinceReader
		serviceChangedSince query.ServiceChangedSinceReader
	)
	if db != nil {
		generationLifecycle = pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db})
		changedSince = pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db})
		serviceChangedSince = pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db})
	}
	return &query.FreshnessHandler{
		Generations:         generationLifecycle,
		ChangedSince:        changedSince,
		ServiceChangedSince: serviceChangedSince,
		Profile:             profile,
	}
}
