// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the correlation families (servicecatalog, cicdrun, crossrepo, sbomattest)
// (issue #6061). It merges the per-family *_compat.go files listed below
// with no behavior change: every alias and forwarder is preserved
// byte-identical under its stanza marker. A family move adds a stanza
// to the matching bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md). Each entry is deleted
// once its last caller has moved; see the importer-migration child issue.
//
// Stanzas merged here:
//   - service_catalog_correlation_compat.go
//   - ci_cd_run_correlation_compat.go
//   - cross_repo_compat.go
//   - sbom_attestation_attachment_compat.go

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sbomattest"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Stanza: service_catalog_correlation_compat.go (merged; do not recreate this file).
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

// Stanza: ci_cd_run_correlation_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the CI/CD run
// correlation domain that moved to [cicdrun] (issue #6061). Reducer-root call
// sites and the external packages that name these types keep their current
// spelling; each entry is deleted once its last caller has moved into a
// family subpackage.

// CICDRunCorrelationOutcome forwards to [cicdrun.CICDRunCorrelationOutcome].
type CICDRunCorrelationOutcome = cicdrun.CICDRunCorrelationOutcome

// The CICDRunCorrelation outcome values forward to their [cicdrun] equivalents.
const (
	CICDRunCorrelationExact      = cicdrun.CICDRunCorrelationExact
	CICDRunCorrelationDerived    = cicdrun.CICDRunCorrelationDerived
	CICDRunCorrelationAmbiguous  = cicdrun.CICDRunCorrelationAmbiguous
	CICDRunCorrelationUnresolved = cicdrun.CICDRunCorrelationUnresolved
	CICDRunCorrelationRejected   = cicdrun.CICDRunCorrelationRejected
)

// CICDRunCorrelationDecision forwards to [cicdrun.CICDRunCorrelationDecision].
type CICDRunCorrelationDecision = cicdrun.CICDRunCorrelationDecision

// CICDRunCorrelationWrite forwards to [cicdrun.CICDRunCorrelationWrite].
type CICDRunCorrelationWrite = cicdrun.CICDRunCorrelationWrite

// CICDRunCorrelationWriteResult forwards to
// [cicdrun.CICDRunCorrelationWriteResult].
type CICDRunCorrelationWriteResult = cicdrun.CICDRunCorrelationWriteResult

// CICDRunCorrelationWriter forwards to [cicdrun.CICDRunCorrelationWriter].
type CICDRunCorrelationWriter = cicdrun.CICDRunCorrelationWriter

// CICDRunCorrelationHandler forwards to [cicdrun.CICDRunCorrelationHandler].
type CICDRunCorrelationHandler = cicdrun.CICDRunCorrelationHandler

// PostgresCICDRunCorrelationWriter forwards to
// [cicdrun.PostgresCICDRunCorrelationWriter].
type PostgresCICDRunCorrelationWriter = cicdrun.PostgresCICDRunCorrelationWriter

// cicdRunCorrelationFactKind forwards to [cicdrun.CICDRunCorrelationFactKind].
const cicdRunCorrelationFactKind = cicdrun.CICDRunCorrelationFactKind

// cicdWorkflowImageBuiltFromEvidenceSource forwards to
// [cicdrun.CICDWorkflowImageBuiltFromEvidenceSource].
const cicdWorkflowImageBuiltFromEvidenceSource = cicdrun.CICDWorkflowImageBuiltFromEvidenceSource

// BuildCICDRunCorrelationDecisions forwards to
// [cicdrun.BuildCICDRunCorrelationDecisions].
func BuildCICDRunCorrelationDecisions(envelopes []facts.Envelope) []CICDRunCorrelationDecision {
	return cicdrun.BuildCICDRunCorrelationDecisions(envelopes)
}

// Stanza: cross_repo_compat.go (merged; do not recreate this file).
// The cross-repo resolution family moved to [crossrepo] under issue #6061.
// These aliases keep the reducer root's own callers -- and the packages that
// name them through this package (cmd/reducer, internal/ifa/materializededges,
// internal/storage/cypher) -- compiling against the same spellings. The
// dependency runs root -> family only; the family never imports this package.

// CrossRepoEvidenceSource is the evidence_source the cross-repo resolver stamps
// on every edge it writes. Alias for [crossrepo.CrossRepoEvidenceSource].
const CrossRepoEvidenceSource = crossrepo.CrossRepoEvidenceSource

// EvidenceFactLoader loads persisted evidence facts for a generation.
// Alias for [crossrepo.EvidenceFactLoader].
type EvidenceFactLoader = crossrepo.EvidenceFactLoader

// AssertionLoader loads relationship assertions.
// Alias for [crossrepo.AssertionLoader].
type AssertionLoader = crossrepo.AssertionLoader

// ResolutionPersister persists resolution outputs and activates the generation.
// Alias for [crossrepo.ResolutionPersister].
type ResolutionPersister = crossrepo.ResolutionPersister

// RepoDependencyIntentWriter persists durable repo-dependency projection
// intents. Alias for [crossrepo.RepoDependencyIntentWriter].
type RepoDependencyIntentWriter = crossrepo.RepoDependencyIntentWriter

// CrossRepoRelationshipHandler resolves cross-repository relationships from
// persisted evidence facts. Alias for [crossrepo.CrossRepoRelationshipHandler].
type CrossRepoRelationshipHandler = crossrepo.CrossRepoRelationshipHandler

// ExtractRepoDependencyIntentRows exposes the resolved-relationship to
// intent-row conversion for Ifá's materialized-edge vacuity guards. Forwards to
// [crossrepo.ExtractRepoDependencyIntentRows].
func ExtractRepoDependencyIntentRows(
	resolved []relationships.ResolvedRelationship,
	scopeID string,
	sourceRunID string,
	generationID string,
	createdAt time.Time,
) ([]sharedintent.Row, map[string]int) {
	return crossrepo.ExtractRepoDependencyIntentRows(resolved, scopeID, sourceRunID, generationID, createdAt)
}

// Stanza: sbom_attestation_attachment_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the sbom_attestation
// attachment family that moved to [sbomattest] (issue #6061). Reducer-root call
// sites and the external packages that name these types keep their current
// spelling; each entry is deleted once its last caller has moved into a family
// subpackage.

// SBOMAttachmentStatus names the reducer decision for one SBOM or attestation
// document attachment. See [sbomattest.SBOMAttachmentStatus].
type SBOMAttachmentStatus = sbomattest.SBOMAttachmentStatus

const (
	// SBOMAttachmentAttachedVerified forwards to
	// [sbomattest.SBOMAttachmentAttachedVerified].
	SBOMAttachmentAttachedVerified = sbomattest.SBOMAttachmentAttachedVerified
	// SBOMAttachmentAttachedUnverified forwards to
	// [sbomattest.SBOMAttachmentAttachedUnverified].
	SBOMAttachmentAttachedUnverified = sbomattest.SBOMAttachmentAttachedUnverified
	// SBOMAttachmentAttachedParseOnly forwards to
	// [sbomattest.SBOMAttachmentAttachedParseOnly].
	SBOMAttachmentAttachedParseOnly = sbomattest.SBOMAttachmentAttachedParseOnly
	// SBOMAttachmentSubjectMismatch forwards to
	// [sbomattest.SBOMAttachmentSubjectMismatch].
	SBOMAttachmentSubjectMismatch = sbomattest.SBOMAttachmentSubjectMismatch
	// SBOMAttachmentAmbiguousSubject forwards to
	// [sbomattest.SBOMAttachmentAmbiguousSubject].
	SBOMAttachmentAmbiguousSubject = sbomattest.SBOMAttachmentAmbiguousSubject
	// SBOMAttachmentUnknownSubject forwards to
	// [sbomattest.SBOMAttachmentUnknownSubject].
	SBOMAttachmentUnknownSubject = sbomattest.SBOMAttachmentUnknownSubject
	// SBOMAttachmentUnparseable forwards to
	// [sbomattest.SBOMAttachmentUnparseable].
	SBOMAttachmentUnparseable = sbomattest.SBOMAttachmentUnparseable
)

// SBOMAttestationAttachmentDecision records one reducer attachment decision.
// See [sbomattest.SBOMAttestationAttachmentDecision].
type SBOMAttestationAttachmentDecision = sbomattest.SBOMAttestationAttachmentDecision

// SBOMAttestationAttachmentWrite carries decisions for durable publication.
// See [sbomattest.SBOMAttestationAttachmentWrite].
type SBOMAttestationAttachmentWrite = sbomattest.SBOMAttestationAttachmentWrite

// SBOMAttestationAttachmentWriteResult summarizes durable publication. See
// [sbomattest.SBOMAttestationAttachmentWriteResult].
type SBOMAttestationAttachmentWriteResult = sbomattest.SBOMAttestationAttachmentWriteResult

// SBOMAttestationAttachmentWriter persists reducer-owned attachment facts.
// See [sbomattest.SBOMAttestationAttachmentWriter].
type SBOMAttestationAttachmentWriter = sbomattest.SBOMAttestationAttachmentWriter

// SBOMAttestationAttachmentHandler attaches SBOM and attestation documents to
// image digests only when subject evidence is explicit. See
// [sbomattest.SBOMAttestationAttachmentHandler].
type SBOMAttestationAttachmentHandler = sbomattest.SBOMAttestationAttachmentHandler

// PostgresSBOMAttestationAttachmentWriter stores reducer-owned SBOM and
// attestation attachment decisions in the shared fact store. See
// [sbomattest.PostgresSBOMAttestationAttachmentWriter].
type PostgresSBOMAttestationAttachmentWriter = sbomattest.PostgresSBOMAttestationAttachmentWriter

// BuildSBOMAttestationAttachmentDecisions forwards to
// [sbomattest.BuildSBOMAttestationAttachmentDecisions].
func BuildSBOMAttestationAttachmentDecisions(envelopes []facts.Envelope) []SBOMAttestationAttachmentDecision {
	return sbomattest.BuildSBOMAttestationAttachmentDecisions(envelopes)
}

// MaxSBOMAttachmentComponentEvidenceRows forwards to
// [sbomattest.MaxSBOMAttachmentComponentEvidenceRows].
const MaxSBOMAttachmentComponentEvidenceRows = sbomattest.MaxSBOMAttachmentComponentEvidenceRows

// ComponentEvidence is the exported field-for-field mirror of the internal
// sbomAttachmentComponentEvidence tuple. See [sbomattest.ComponentEvidence].
type ComponentEvidence = sbomattest.ComponentEvidence

// ComponentEvidenceLess forwards to [sbomattest.ComponentEvidenceLess].
func ComponentEvidenceLess(a, b ComponentEvidence) bool {
	return sbomattest.ComponentEvidenceLess(a, b)
}

// ComponentEvidenceTupleEqual forwards to
// [sbomattest.ComponentEvidenceTupleEqual].
func ComponentEvidenceTupleEqual(a, b ComponentEvidence) bool {
	return sbomattest.ComponentEvidenceTupleEqual(a, b)
}

// payloadStrings forwards to [payloadcore.PayloadStrings]. It reaches the
// shared helper directly rather than through sbomattest: the root callers are
// secrets/IAM, security-alert-reconciliation and supply-chain-impact, none of
// which have anything to do with the SBOM family.
func payloadStrings(payload map[string]any, scalarKey string, sliceKey string) []string {
	return payloadcore.PayloadStrings(payload, scalarKey, sliceKey)
}

// sbomAttestationAttachmentFactKind lives in intent.go, aliased directly from
// [reducercontract.SBOMAttestationAttachmentFactKind] rather than forwarded
// through sbomattest -- see that file's alias block, mirroring
// containerImageIdentityFactKind's identical shape for the same reason
// (#6431).
