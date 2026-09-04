// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
)

// This file is the transitional compatibility surface for the service-catalog
// correlation and service-materialization family that moved to
// [servicecatalog] (issue #6061). It carries only the names that still have a
// caller: the reducer root's own defaults/handler wiring
// (defaults.go, defaults_additive_domains.go, defaults_service_incidents.go,
// defaults_additive_domains_correlation.go, registry_additive_domains.go),
// cmd/reducer's writer construction, internal/storage/postgres' loaders, and
// the still-in-root supply_chain_impact and service_runtime_instance_lookup
// families' shared outcome/evidence vocabulary. Everything else the family
// exports is reached as servicecatalog.X, and each entry here is deleted once
// its last caller has moved.

// ServiceCatalogCorrelationOutcome names the reducer decision for one catalog
// entity. See [servicecatalog.ServiceCatalogCorrelationOutcome].
type ServiceCatalogCorrelationOutcome = servicecatalog.ServiceCatalogCorrelationOutcome

const (
	// ServiceCatalogCorrelationExact means one catalog entity matched one
	// canonical repository through a stable repository identity.
	ServiceCatalogCorrelationExact = servicecatalog.ServiceCatalogCorrelationExact
	// ServiceCatalogCorrelationDerived means one catalog entity matched one
	// canonical repository through deterministic URL canonicalization.
	ServiceCatalogCorrelationDerived = servicecatalog.ServiceCatalogCorrelationDerived
	// ServiceCatalogCorrelationAmbiguous means one catalog entity matched
	// multiple active repositories.
	ServiceCatalogCorrelationAmbiguous = servicecatalog.ServiceCatalogCorrelationAmbiguous
	// ServiceCatalogCorrelationUnresolved means the catalog entity is valid
	// but no repository matched it.
	ServiceCatalogCorrelationUnresolved = servicecatalog.ServiceCatalogCorrelationUnresolved
	// ServiceCatalogCorrelationStale means the catalog entity matched only
	// tombstoned repository facts.
	ServiceCatalogCorrelationStale = servicecatalog.ServiceCatalogCorrelationStale
	// ServiceCatalogCorrelationRejected means the catalog entity cannot
	// participate in correlation.
	ServiceCatalogCorrelationRejected = servicecatalog.ServiceCatalogCorrelationRejected
)

// ServiceCatalogCorrelationDecision records one catalog entity's correlation
// outcome. See [servicecatalog.ServiceCatalogCorrelationDecision].
type ServiceCatalogCorrelationDecision = servicecatalog.ServiceCatalogCorrelationDecision

// ServiceCatalogCorrelationWrite is the durable publication input one
// service-catalog-correlation execution submits. See
// [servicecatalog.ServiceCatalogCorrelationWrite].
type ServiceCatalogCorrelationWrite = servicecatalog.ServiceCatalogCorrelationWrite

// ServiceCatalogCorrelationWriteResult summarizes durable catalog-correlation
// writes. See [servicecatalog.ServiceCatalogCorrelationWriteResult].
type ServiceCatalogCorrelationWriteResult = servicecatalog.ServiceCatalogCorrelationWriteResult

// ServiceCatalogCorrelationWriter persists service-catalog correlation
// decisions. See [servicecatalog.ServiceCatalogCorrelationWriter].
type ServiceCatalogCorrelationWriter = servicecatalog.ServiceCatalogCorrelationWriter

// ServiceCatalogCorrelationHandler correlates catalog declarations against
// active repository facts. See [servicecatalog.ServiceCatalogCorrelationHandler].
type ServiceCatalogCorrelationHandler = servicecatalog.ServiceCatalogCorrelationHandler

// PostgresServiceCatalogCorrelationWriter is the Postgres-backed
// ServiceCatalogCorrelationWriter. See
// [servicecatalog.PostgresServiceCatalogCorrelationWriter].
type PostgresServiceCatalogCorrelationWriter = servicecatalog.PostgresServiceCatalogCorrelationWriter

// BuildServiceCatalogCorrelationDecisions forwards to
// [servicecatalog.BuildServiceCatalogCorrelationDecisions].
func BuildServiceCatalogCorrelationDecisions(envelopes []facts.Envelope) []ServiceCatalogCorrelationDecision {
	return servicecatalog.BuildServiceCatalogCorrelationDecisions(envelopes)
}

// ServiceMaterializationWrite is the additive per-service evidence generation
// lineage write input. See [servicecatalog.ServiceMaterializationWrite].
type ServiceMaterializationWrite = servicecatalog.ServiceMaterializationWrite

// ServiceMaterializationWriteResult summarizes a service materialization
// commit. See [servicecatalog.ServiceMaterializationWriteResult].
type ServiceMaterializationWriteResult = servicecatalog.ServiceMaterializationWriteResult

// ServiceOwnershipEvidence is one owner-ref evidence row before it is
// resolved into a generation-stable snapshot row. See
// [servicecatalog.ServiceOwnershipEvidence].
type ServiceOwnershipEvidence = servicecatalog.ServiceOwnershipEvidence

// Evidence family label constants. See
// [servicecatalog.ServiceEvidenceFamilyOwnership] and its siblings.
const (
	ServiceEvidenceFamilyOwnership       = servicecatalog.ServiceEvidenceFamilyOwnership
	ServiceEvidenceFamilyDeployment      = servicecatalog.ServiceEvidenceFamilyDeployment
	ServiceEvidenceFamilyRuntime         = servicecatalog.ServiceEvidenceFamilyRuntime
	ServiceEvidenceFamilyDependencies    = servicecatalog.ServiceEvidenceFamilyDependencies
	ServiceEvidenceFamilyDocs            = servicecatalog.ServiceEvidenceFamilyDocs
	ServiceEvidenceFamilyIncidents       = servicecatalog.ServiceEvidenceFamilyIncidents
	ServiceEvidenceFamilyVulnerabilities = servicecatalog.ServiceEvidenceFamilyVulnerabilities
)

// ServiceMaterializationWriter commits the additive per-service evidence
// generation lineage. See [servicecatalog.ServiceMaterializationWriter].
type ServiceMaterializationWriter = servicecatalog.ServiceMaterializationWriter

// ServiceMaterializationTx is the narrow transactional surface the lineage
// writer needs. See [servicecatalog.ServiceMaterializationTx].
type ServiceMaterializationTx = servicecatalog.ServiceMaterializationTx

// ServiceMaterializationRow is the narrow *sql.Row surface the lineage
// writer's QueryRowContext needs. See [servicecatalog.ServiceMaterializationRow].
type ServiceMaterializationRow = servicecatalog.ServiceMaterializationRow

// ServiceMaterializationBeginner begins a ServiceMaterializationTx. See
// [servicecatalog.ServiceMaterializationBeginner].
type ServiceMaterializationBeginner = servicecatalog.ServiceMaterializationBeginner

// PostgresServiceMaterializationWriter commits the additive per-service
// evidence generation lineage against Postgres. See
// [servicecatalog.PostgresServiceMaterializationWriter].
type PostgresServiceMaterializationWriter = servicecatalog.PostgresServiceMaterializationWriter

// ServiceMaterializationGenerationID forwards to
// [servicecatalog.ServiceMaterializationGenerationID].
// service_changed_since_golden_fixture_test.go asserts distinctness and
// idempotency directly against this derivation.
func ServiceMaterializationGenerationID(write ServiceMaterializationWrite) string {
	return servicecatalog.ServiceMaterializationGenerationID(write)
}

// ServiceOwnershipEvidenceKey forwards to
// [servicecatalog.ServiceOwnershipEvidenceKey].
func ServiceOwnershipEvidenceKey(serviceID, ownerRef string) string {
	return servicecatalog.ServiceOwnershipEvidenceKey(serviceID, ownerRef)
}

// ServiceEvidencePayloadHash forwards to
// [servicecatalog.ServiceEvidencePayloadHash].
func ServiceEvidencePayloadHash(payload map[string]any) string {
	return servicecatalog.ServiceEvidencePayloadHash(payload)
}

// Service materialization generation status values. See
// [servicecatalog.ServiceMaterializationStatusActive] and its siblings.
const (
	ServiceMaterializationStatusPending    = servicecatalog.ServiceMaterializationStatusPending
	ServiceMaterializationStatusActive     = servicecatalog.ServiceMaterializationStatusActive
	ServiceMaterializationStatusSuperseded = servicecatalog.ServiceMaterializationStatusSuperseded
)

// RepositoryScopedRuntimeInstanceLoader supplies materialized runtime
// instances for a repository. See
// [servicecatalog.RepositoryScopedRuntimeInstanceLoader].
type RepositoryScopedRuntimeInstanceLoader = servicecatalog.RepositoryScopedRuntimeInstanceLoader

// ServiceRuntimeInstance is one materialized runtime instance of a service's
// workload. See [servicecatalog.ServiceRuntimeInstance].
type ServiceRuntimeInstance = servicecatalog.ServiceRuntimeInstance

// ServiceRuntimeEvidenceKey forwards to
// [servicecatalog.ServiceRuntimeEvidenceKey].
func ServiceRuntimeEvidenceKey(serviceID string, instance ServiceRuntimeInstance) string {
	return servicecatalog.ServiceRuntimeEvidenceKey(serviceID, instance)
}

// ServiceScopedDocumentationEvidenceLoader supplies documentation evidence
// for a set of services. See
// [servicecatalog.ServiceScopedDocumentationEvidenceLoader].
type ServiceScopedDocumentationEvidenceLoader = servicecatalog.ServiceScopedDocumentationEvidenceLoader

// ServiceScopedIncidentEvidenceLoader supplies incident-routing evidence for
// a set of services. See
// [servicecatalog.ServiceScopedIncidentEvidenceLoader].
type ServiceScopedIncidentEvidenceLoader = servicecatalog.ServiceScopedIncidentEvidenceLoader

// ServiceVulnerabilityAdvisoryLoader supplies supply-chain advisory findings
// for a set of repositories. See
// [servicecatalog.ServiceVulnerabilityAdvisoryLoader].
type ServiceVulnerabilityAdvisoryLoader = servicecatalog.ServiceVulnerabilityAdvisoryLoader

// ServiceDocumentationRecord is one documentation fact referencing a service,
// as internal/storage/postgres' loader returns it. See
// [servicecatalog.ServiceDocumentationRecord].
type ServiceDocumentationRecord = servicecatalog.ServiceDocumentationRecord

// ServiceIncidentRecord is one incident-routing evidence row referencing a
// service, as internal/storage/postgres' loader returns it. See
// [servicecatalog.ServiceIncidentRecord].
type ServiceIncidentRecord = servicecatalog.ServiceIncidentRecord

// ServiceVulnerabilityRecord is one supply-chain advisory finding referencing
// a service's repository, as internal/storage/postgres' loader returns it.
// See [servicecatalog.ServiceVulnerabilityRecord].
type ServiceVulnerabilityRecord = servicecatalog.ServiceVulnerabilityRecord
