// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"github.com/eshu-hq/eshu/go/internal/query/kubernetes"
	"github.com/eshu-hq/eshu/go/internal/query/metrics"
	"github.com/eshu-hq/eshu/go/internal/query/observability/coverage"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/secrets"
	"github.com/eshu-hq/eshu/go/internal/query/semanticsearch"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/workitem"
)

// capabilitySupport is the per-profile truth ceiling contract each
// registration in this package declares. It aliases querycontract's type so
// the rows read the same as they did in the query root they moved from.
type capabilitySupport = querycontract.CapabilitySupport

// Profile names, aliased so the moved rows keep their original spelling.
const (
	ProfileLocalLightweight   = querycontract.ProfileLocalLightweight
	ProfileLocalAuthoritative = querycontract.ProfileLocalAuthoritative
	ProfileLocalFullStack     = querycontract.ProfileLocalFullStack
	ProfileProduction         = querycontract.ProfileProduction
)

// Truth levels, aliased for the same reason.
const (
	TruthLevelExact   = querycontract.TruthLevelExact
	TruthLevelDerived = querycontract.TruthLevelDerived
)

// register records one capability row. It calls RegisterCapabilities rather
// than writing the registry map directly, which is the difference that matters
// in this move: a direct write skips the duplicate and ordering bookkeeping, so
// a repeated key went unnoticed by querycontract_boundary_test.go's
// DuplicateCapabilityRegistrations assertion. Every row in this package is now
// counted by it.
func register(capability string, support capabilitySupport) {
	querycontract.RegisterCapabilities(querycontract.CapabilityRegistration{
		Capability: capability,
		Support:    support,
	})
}

const (
	// Forwards to the family that owns the route.
	containerImageIdentitiesCapability               = supplychain.ContainerImageIdentitiesCapability
	containerImageIdentityAggregateCapability        = supplychain.ContainerImageIdentityAggregateCapability
	sbomAttestationAttachmentAggregateCapability     = supplychain.SBOMAttestationAttachmentAggregateCapability
	sbomAttestationAttachmentsCapability             = supplychain.SBOMAttestationAttachmentsCapability
	securityAlertReconciliationAggregateCapability   = supplychain.SecurityAlertReconciliationAggregateCapability
	securityAlertReconciliationsCapability           = supplychain.SecurityAlertReconciliationsCapability
	supplyChainImpactAggregateCapability             = supplychain.ImpactAggregateCapability
	supplyChainImpactExplanationCapability           = supplychain.ImpactExplanationCapability
	supplyChainImpactFindingsCapability              = supplychain.ImpactFindingsCapability
	vulnerabilityScannerReadContractCapability       = supplychain.VulnerabilityScannerReadContractCapability
	secretsIAMIdentityTrustChainsCapability          = secrets.IAMIdentityTrustChainsCapability
	secretsIAMPostureGapsCapability                  = secrets.IAMPostureGapsCapability
	secretsIAMPostureSummaryCapability               = secrets.IAMPostureSummaryCapability
	secretsIAMPrivilegePostureObservationsCapability = secrets.IAMPrivilegePostureObservationsCapability
	secretsIAMSecretAccessPathsCapability            = secrets.IAMSecretAccessPathsCapability
	semanticSearchCapability                         = semanticsearch.Capability
	workItemEvidenceCapability                       = workitem.EvidenceCapability
)

const (
	// Routes still handled in the query root; literal by design, see above.
	CapabilityQueryPlaybooks                     = "query.playbooks"
	CapabilityInvestigationWorkflows             = "query.investigation_workflows"
	admissionDecisionCapability                  = "admission_decisions.list"
	answerNarrationStatusCapability              = "answer_narration.status"
	cicdRunCorrelationAggregateCapability        = "ci_cd.run_correlations.aggregate"
	cicdRunCorrelationsCapability                = "ci_cd.run_correlations.list"
	cloudInventoryReadbackCapability             = "cloud_inventory.readback.list"
	cloudRuntimeDriftReadbackCapability          = "cloud_runtime_drift.readback.list"
	collectorExtractionReadinessFamilyCapability = "collector_extraction_readiness.family"
	collectorExtractionReadinessListCapability   = "collector_extraction_readiness.list"
	factSchemaVersionDetailCapability            = "fact_schema_version.detail"
	factSchemaVersionListCapability              = "fact_schema_version.list"
	hostedGovernanceStatusCapability             = "hosted_governance.status"
	kubernetesCorrelationsCapability             = kubernetes.Capability
	metricsTimeSeriesCapability                  = metrics.Capability
	observabilityCoverageCorrelationsCapability  = coverage.Capability
	operationsStatusCapability                   = "operations.status"
	replatformingOwnershipCapability             = "replatforming.ownership.candidates"
	replatformingRollupsCapability               = "replatforming.rollups.readiness"
	semanticCodeHintsCapability                  = "semantic_evidence.code_hints.list"
	semanticDocumentationObservationsCapability  = "semantic_evidence.documentation_observations.list"
	semanticExtractionStatusCapability           = "semantic_extraction.status"
)
