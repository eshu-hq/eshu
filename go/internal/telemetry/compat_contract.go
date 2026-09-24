// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// Compat surface for go/internal/telemetry/contract (issue #6777). The
// per-family span, metric-dimension, and log-key declarations that used to
// live directly in this package as contract_*.go now live in the contract
// subpackage; every name below re-exports one of them as a root telemetry.*
// identifier so every existing caller across the repo keeps compiling
// unchanged. Do not add doc comments here that duplicate the contract
// package's canonical documentation — see the referenced contract.* symbol
// for the authoritative comment. Delete a stanza only once its last root-level
// caller has migrated to importing the contract subpackage directly.

// Admission-decisions query span (from contract/admission_decisions.go).
const SpanQueryAdmissionDecisions = contract.SpanQueryAdmissionDecisions

// Azure relationship materialization span (from contract/azure_relationship.go).
const SpanReducerAzureRelationshipMaterialization = contract.SpanReducerAzureRelationshipMaterialization

// Bootstrap collector-cycle span (from contract/bootstrap_ingestion.go).
const SpanBootstrapCollectorCycle = contract.SpanBootstrapCollectorCycle

// CI/CD run source-collector and correlation spans (from contract/cicd.go).
const (
	SpanCICDRunObserve                   = contract.SpanCICDRunObserve
	SpanCICDRunFetch                     = contract.SpanCICDRunFetch
	SpanQueryCICDRunCorrelations         = contract.SpanQueryCICDRunCorrelations
	SpanQueryCICDRunCorrelationAggregate = contract.SpanQueryCICDRunCorrelationAggregate
)

// Claimed-service run outcome and span (from contract/collector_run.go).
const (
	CollectorRunOutcomeSuccess       = contract.CollectorRunOutcomeSuccess
	CollectorRunOutcomeUnchanged     = contract.CollectorRunOutcomeUnchanged
	CollectorRunOutcomeReleased      = contract.CollectorRunOutcomeReleased
	CollectorRunOutcomeFailRetryable = contract.CollectorRunOutcomeFailRetryable
	CollectorRunOutcomeFailTerminal  = contract.CollectorRunOutcomeFailTerminal
	SpanCollectorClaimedRun          = contract.SpanCollectorClaimedRun
)

// Git-collector snapshot stage dimension, values, and span (from
// contract/collector_stage.go).
const (
	MetricDimensionStage                  = contract.MetricDimensionStage
	SnapshotStageDiscovery                = contract.SnapshotStageDiscovery
	SnapshotStagePreScan                  = contract.SnapshotStagePreScan
	SnapshotStageGoPackageSemanticPreScan = contract.SnapshotStageGoPackageSemanticPreScan
	SnapshotStageParse                    = contract.SnapshotStageParse
	SnapshotStageMaterialize              = contract.SnapshotStageMaterialize
	SnapshotStageValueFlowEvidence        = contract.SnapshotStageValueFlowEvidence
	SpanCollectorSnapshotStage            = contract.SpanCollectorSnapshotStage
)

// AttrStage forwards to contract.AttrStage.
func AttrStage(v string) attribute.KeyValue { return contract.AttrStage(v) }

// Neo4jReader graph-read outcome span attributes (from contract/graph_read.go).
const (
	SpanAttrGraphReadOutcome              = contract.SpanAttrGraphReadOutcome
	SpanAttrGraphReadAttempts             = contract.SpanAttrGraphReadAttempts
	SpanAttrGraphReadConfiguredDeadlineMS = contract.SpanAttrGraphReadConfiguredDeadlineMS
	SpanAttrGraphReadStatementFingerprint = contract.SpanAttrGraphReadStatementFingerprint
	LogKeyGraphReadStatementFingerprint   = contract.LogKeyGraphReadStatementFingerprint
	LogKeyGraphReadStatementHead          = contract.LogKeyGraphReadStatementHead
)

// Kubernetes correlation query span (from contract/kubernetes.go).
const SpanQueryKubernetesCorrelations = contract.SpanQueryKubernetesCorrelations

// Language-query route span (from contract/language_query.go).
const SpanQueryLanguageQuery = contract.SpanQueryLanguageQuery

// Parser/SCIP language dimension (from contract/language.go).
const MetricDimensionLanguage = contract.MetricDimensionLanguage

// AttrLanguage forwards to contract.AttrLanguage.
func AttrLanguage(v string) attribute.KeyValue { return contract.AttrLanguage(v) }

// Package-registry correlation, dependency-chain, and aggregate spans (from
// contract/package_registry.go).
const (
	SpanQueryPackageRegistryCorrelations     = contract.SpanQueryPackageRegistryCorrelations
	SpanQueryPackageRegistryDependencyChains = contract.SpanQueryPackageRegistryDependencyChains
	SpanQueryPackageRegistryAggregate        = contract.SpanQueryPackageRegistryAggregate
)

// Prompt-facing investigation route spans (from contract/query_spans.go).
const (
	SpanQueryHardcodedSecretInvestigation  = contract.SpanQueryHardcodedSecretInvestigation
	SpanQueryImportDependencyInvestigation = contract.SpanQueryImportDependencyInvestigation
	SpanQueryCallGraphMetrics              = contract.SpanQueryCallGraphMetrics
	SpanQueryGraphSummaryPacket            = contract.SpanQueryGraphSummaryPacket
	SpanQueryGraphEntityInventory          = contract.SpanQueryGraphEntityInventory
)

// S3 external-principal grant materialization span (from
// contract/s3_external_principal.go).
const SpanReducerS3ExternalPrincipalGrantMaterialization = contract.SpanReducerS3ExternalPrincipalGrantMaterialization

// Scanner-worker dimensions and spans (from contract/scanner_worker.go).
const (
	MetricDimensionAnalyzer        = contract.MetricDimensionAnalyzer
	MetricDimensionTargetKind      = contract.MetricDimensionTargetKind
	MetricDimensionLimitKind       = contract.MetricDimensionLimitKind
	SpanScannerWorkerClaimProcess  = contract.SpanScannerWorkerClaimProcess
	SpanScannerWorkerAnalyze       = contract.SpanScannerWorkerAnalyze
	SpanScannerWorkerFactEmitBatch = contract.SpanScannerWorkerFactEmitBatch
)

// Hosted-provider security-alert spans (from contract/security_alert.go).
const (
	SpanSecurityAlertObserve     = contract.SpanSecurityAlertObserve
	SpanSecurityAlertFetch       = contract.SpanSecurityAlertFetch
	AttrSecurityAlertTargetScope = contract.AttrSecurityAlertTargetScope
)

// Semantic-extraction dimensions, queue spans, and log keys (from
// contract/semantic_extraction.go).
const (
	MetricDimensionSourceClass                   = contract.MetricDimensionSourceClass
	MetricDimensionProviderKind                  = contract.MetricDimensionProviderKind
	MetricDimensionProviderProfileClass          = contract.MetricDimensionProviderProfileClass
	MetricDimensionBudgetState                   = contract.MetricDimensionBudgetState
	MetricDimensionBudgetReason                  = contract.MetricDimensionBudgetReason
	SpanSemanticExtractionQueueApply             = contract.SpanSemanticExtractionQueueApply
	SpanSemanticExtractionQueueClaim             = contract.SpanSemanticExtractionQueueClaim
	SpanSemanticExtractionQueueComplete          = contract.SpanSemanticExtractionQueueComplete
	LogKeySemanticExtractionStatus               = contract.LogKeySemanticExtractionStatus
	LogKeySemanticExtractionSourceClass          = contract.LogKeySemanticExtractionSourceClass
	LogKeySemanticExtractionProviderKind         = contract.LogKeySemanticExtractionProviderKind
	LogKeySemanticExtractionProviderProfileClass = contract.LogKeySemanticExtractionProviderProfileClass
	LogKeySemanticExtractionBudgetState          = contract.LogKeySemanticExtractionBudgetState
	LogKeySemanticExtractionBudgetReason         = contract.LogKeySemanticExtractionBudgetReason
)

// Service-catalog correlation query span (from contract/service_catalog.go).
const SpanQueryServiceCatalogCorrelations = contract.SpanQueryServiceCatalogCorrelations

// Graph-provenance source-tool dimension (from contract/source_tool.go).
const MetricDimensionSourceTool = contract.MetricDimensionSourceTool

// AttrSourceTool forwards to contract.AttrSourceTool.
func AttrSourceTool(v string) attribute.KeyValue { return contract.AttrSourceTool(v) }

// Supply-chain / vulnerability-impact query spans, mutation outcome
// attributes, and outcome vocabulary (from contract/supply_chain.go).
const (
	SpanQuerySBOMAttestationAttachments               = contract.SpanQuerySBOMAttestationAttachments
	SpanQueryAdvisoryEvidence                         = contract.SpanQueryAdvisoryEvidence
	SpanQueryAdvisoryCatalog                          = contract.SpanQueryAdvisoryCatalog
	SpanQuerySupplyChainImpactFindings                = contract.SpanQuerySupplyChainImpactFindings
	SpanQueryVulnerabilitySuppressionMutation         = contract.SpanQueryVulnerabilitySuppressionMutation
	SpanAttrVulnerabilitySuppressionMutationOutcome   = contract.SpanAttrVulnerabilitySuppressionMutationOutcome
	VulnerabilitySuppressionMutationOutcomeCreated    = contract.VulnerabilitySuppressionMutationOutcomeCreated
	VulnerabilitySuppressionMutationOutcomeUnchanged  = contract.VulnerabilitySuppressionMutationOutcomeUnchanged
	VulnerabilitySuppressionMutationOutcomeRejected   = contract.VulnerabilitySuppressionMutationOutcomeRejected
	VulnerabilitySuppressionMutationOutcomeStoreError = contract.VulnerabilitySuppressionMutationOutcomeStoreError
	SpanQuerySupplyChainImpactExplanation             = contract.SpanQuerySupplyChainImpactExplanation
	SpanQueryContainerImageIdentities                 = contract.SpanQueryContainerImageIdentities
	SpanQuerySupplyChainSecurityAlerts                = contract.SpanQuerySupplyChainSecurityAlerts
	SpanQuerySupplyChainImpactAggregate               = contract.SpanQuerySupplyChainImpactAggregate
	SpanQuerySecurityAlertReconciliationAggregate     = contract.SpanQuerySecurityAlertReconciliationAggregate
	SpanQueryContainerImageIdentityAggregate          = contract.SpanQueryContainerImageIdentityAggregate
	SpanQuerySBOMAttestationAttachmentAggregate       = contract.SpanQuerySBOMAttestationAttachmentAggregate
)

// VulnerabilitySuppressionMutationOutcomes forwards to
// contract.VulnerabilitySuppressionMutationOutcomes.
func VulnerabilitySuppressionMutationOutcomes() []string {
	return contract.VulnerabilitySuppressionMutationOutcomes()
}

// Vulnerability-intelligence source-collector spans (from
// contract/vulnerability_intelligence.go).
const (
	SpanVulnerabilityIntelligenceObserve = contract.SpanVulnerabilityIntelligenceObserve
	SpanVulnerabilityIntelligenceFetch   = contract.SpanVulnerabilityIntelligenceFetch
)

// Incident-context query span (from contract/z_incident_context.go).
const SpanQueryIncidentContext = contract.SpanQueryIncidentContext

// Observability-coverage correlation query span (from
// contract/z_observability_coverage.go).
const SpanQueryObservabilityCoverageCorrelations = contract.SpanQueryObservabilityCoverageCorrelations

// Secrets/IAM query spans (from contract/z_secrets_iam.go).
const (
	SpanQuerySecretsIAMIdentityTrustChains          = contract.SpanQuerySecretsIAMIdentityTrustChains
	SpanQuerySecretsIAMPrivilegePostureObservations = contract.SpanQuerySecretsIAMPrivilegePostureObservations
	SpanQuerySecretsIAMSecretAccessPaths            = contract.SpanQuerySecretsIAMSecretAccessPaths
	SpanQuerySecretsIAMPostureGaps                  = contract.SpanQuerySecretsIAMPostureGaps
	SpanQuerySecretsIAMPostureSummary               = contract.SpanQuerySecretsIAMPostureSummary
)

// Work-item evidence query span and route span attributes (from
// contract/zz_work_item_evidence.go).
const (
	SpanQueryWorkItemEvidence                          = contract.SpanQueryWorkItemEvidence
	SpanAttrWorkItemEvidenceQueryCount                 = contract.SpanAttrWorkItemEvidenceQueryCount
	SpanAttrWorkItemEvidenceResultCount                = contract.SpanAttrWorkItemEvidenceResultCount
	SpanAttrWorkItemEvidenceStaleCount                 = contract.SpanAttrWorkItemEvidenceStaleCount
	SpanAttrWorkItemEvidencePermissionHiddenCount      = contract.SpanAttrWorkItemEvidencePermissionHiddenCount
	SpanAttrWorkItemEvidenceRejectedUnsafePayloadCount = contract.SpanAttrWorkItemEvidenceRejectedUnsafePayloadCount
	SpanAttrWorkItemEvidenceUnsupportedLinkTypeCount   = contract.SpanAttrWorkItemEvidenceUnsupportedLinkTypeCount
	SpanAttrWorkItemEvidenceMetadataWarningCount       = contract.SpanAttrWorkItemEvidenceMetadataWarningCount
	SpanAttrWorkItemEvidenceMissingCount               = contract.SpanAttrWorkItemEvidenceMissingCount
	SpanAttrWorkItemEvidenceTruncated                  = contract.SpanAttrWorkItemEvidenceTruncated
)

// Generation-lifecycle drilldown query span and attributes (from
// contract/zzz_generation_lifecycle.go).
const (
	SpanQueryFreshnessGenerationLifecycle   = contract.SpanQueryFreshnessGenerationLifecycle
	SpanAttrGenerationLifecycleResultCount  = contract.SpanAttrGenerationLifecycleResultCount
	SpanAttrGenerationLifecycleTruncated    = contract.SpanAttrGenerationLifecycleTruncated
	SpanAttrGenerationLifecycleActiveCount  = contract.SpanAttrGenerationLifecycleActiveCount
	SpanAttrGenerationLifecycleFailureCount = contract.SpanAttrGenerationLifecycleFailureCount
)

// Repository-scope changed-since query span and attributes (from
// contract/zzzz_changed_since.go).
const (
	SpanQueryFreshnessChangedSince          = contract.SpanQueryFreshnessChangedSince
	SpanAttrChangedSinceScopeID             = contract.SpanAttrChangedSinceScopeID
	SpanAttrChangedSinceSinceGenerationID   = contract.SpanAttrChangedSinceSinceGenerationID
	SpanAttrChangedSinceCurrentGenerationID = contract.SpanAttrChangedSinceCurrentGenerationID
	SpanAttrChangedSinceChangedCount        = contract.SpanAttrChangedSinceChangedCount
	SpanAttrChangedSinceUnavailable         = contract.SpanAttrChangedSinceUnavailable
)

// Service-scope changed-since query span, attributes, and the closed
// grant-refusal vocabulary (from contract/zzzz_service_changed_since.go).
const (
	SpanQueryFreshnessServiceChangedSince          = contract.SpanQueryFreshnessServiceChangedSince
	SpanAttrServiceChangedSinceServiceID           = contract.SpanAttrServiceChangedSinceServiceID
	SpanAttrServiceChangedSinceSinceGenerationID   = contract.SpanAttrServiceChangedSinceSinceGenerationID
	SpanAttrServiceChangedSinceCurrentGenerationID = contract.SpanAttrServiceChangedSinceCurrentGenerationID
	SpanAttrServiceChangedSinceChangedCount        = contract.SpanAttrServiceChangedSinceChangedCount
	SpanAttrServiceChangedSinceUnavailable         = contract.SpanAttrServiceChangedSinceUnavailable
	SpanAttrServiceChangedSinceGrantRefused        = contract.SpanAttrServiceChangedSinceGrantRefused
	SpanAttrServiceChangedSinceGrantRefusedReason  = contract.SpanAttrServiceChangedSinceGrantRefusedReason

	ServiceChangedSinceGrantRefusalEmptyGrant       = contract.ServiceChangedSinceGrantRefusalEmptyGrant
	ServiceChangedSinceGrantRefusalNotGranted       = contract.ServiceChangedSinceGrantRefusalNotGranted
	ServiceChangedSinceGrantRefusalSharedOwnership  = contract.ServiceChangedSinceGrantRefusalSharedOwnership
	ServiceChangedSinceGrantRefusalOwnershipUnwired = contract.ServiceChangedSinceGrantRefusalOwnershipUnwired
)
