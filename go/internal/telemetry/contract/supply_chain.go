// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

const (
	// SpanQuerySBOMAttestationAttachments wraps reducer-owned SBOM and
	// attestation attachment reads from durable facts.
	SpanQuerySBOMAttestationAttachments = "query.sbom_attestation_attachments"
	// SpanQueryAdvisoryEvidence wraps source-only vulnerability advisory
	// evidence reads from active source facts.
	SpanQueryAdvisoryEvidence = "query.advisory_evidence"
	// SpanQueryAdvisoryCatalog wraps the browsable, summary-only catalog read
	// over active vulnerability source facts. It powers the unanchored
	// CVE-intelligence list that the console browses.
	SpanQueryAdvisoryCatalog = "query.advisory_catalog"
	// SpanQuerySupplyChainImpactFindings wraps reducer-owned vulnerability
	// impact finding reads from durable facts.
	SpanQuerySupplyChainImpactFindings = "query.supply_chain_impact_findings"
	// SpanQueryVulnerabilitySuppressionMutation wraps authenticated operator
	// writes that create immutable suppression fact generations.
	SpanQueryVulnerabilitySuppressionMutation = "query.vulnerability_suppression_mutation"
	// SpanAttrVulnerabilitySuppressionMutationOutcome records the closed result
	// of an authenticated suppression mutation.
	SpanAttrVulnerabilitySuppressionMutationOutcome = "eshu.mutation.outcome"
	// VulnerabilitySuppressionMutationOutcomeCreated means a new immutable
	// generation and projection intent committed.
	VulnerabilitySuppressionMutationOutcomeCreated = "created"
	// VulnerabilitySuppressionMutationOutcomeUnchanged means an identical retry
	// reused the current generation without another enqueue.
	VulnerabilitySuppressionMutationOutcomeUnchanged = "unchanged"
	// VulnerabilitySuppressionMutationOutcomeRejected means authorization or
	// request validation failed before storage.
	VulnerabilitySuppressionMutationOutcomeRejected = "rejected"
	// VulnerabilitySuppressionMutationOutcomeStoreError means the transactional
	// generation write or enqueue failed.
	VulnerabilitySuppressionMutationOutcomeStoreError = "store_error"
	// SpanQuerySupplyChainImpactExplanation wraps one bounded vulnerability
	// impact explanation over reducer-owned facts and referenced evidence ids.
	SpanQuerySupplyChainImpactExplanation = "query.supply_chain_impact_explanation"
	// SpanQueryContainerImageIdentities wraps reducer-owned image identity
	// reads from durable facts.
	SpanQueryContainerImageIdentities = "query.container_image_identities"
	// SpanQuerySupplyChainSecurityAlerts wraps reducer-owned provider alert
	// reconciliation reads from durable facts.
	SpanQuerySupplyChainSecurityAlerts = "query.supply_chain_security_alerts"
	// SpanQuerySupplyChainImpactAggregate wraps cheap-summary count and
	// inventory aggregates over reducer-owned impact findings. Replaces the
	// page-and-iterate caller pattern for ecosystem-level totals questions.
	SpanQuerySupplyChainImpactAggregate = "query.supply_chain_impact_aggregate"
	// SpanQuerySecurityAlertReconciliationAggregate wraps cheap-summary count
	// and inventory aggregates over reducer-owned provider alert
	// reconciliations. Replaces the page-and-iterate caller pattern for
	// ecosystem-level questions about provider alerts vs Eshu impact state.
	SpanQuerySecurityAlertReconciliationAggregate = "query.security_alert_reconciliation_aggregate"
	// SpanQueryContainerImageIdentityAggregate wraps cheap-summary count and
	// inventory aggregates over reducer-owned container image identities.
	// Replaces the page-and-iterate caller pattern for ecosystem-level
	// questions like "how many images resolved by exact digest vs tag?".
	SpanQueryContainerImageIdentityAggregate = "query.container_image_identity_aggregate"
	// SpanQuerySBOMAttestationAttachmentAggregate wraps cheap-summary count
	// and inventory aggregates over reducer-owned SBOM and attestation
	// attachments. Replaces the page-and-iterate caller pattern for
	// ecosystem-level questions like "how many attestations are verified vs
	// unverified?".
	SpanQuerySBOMAttestationAttachmentAggregate = "query.sbom_attestation_attachment_aggregate"
)

// Supply-chain impact active-evidence load telemetry (#8880). One load resolves
// fact IDs from per-branch, index-anchored statements, then fetches payloads
// by primary key; these instruments time and count each statement so an
// operator can name the branch that regressed.
//
//   - eshu_dp_supply_chain_impact_active_evidence_branch_seconds (branch) —
//     wall time of one statement.
//   - eshu_dp_supply_chain_impact_active_evidence_branch_rows_total (branch) —
//     rows one statement returned.
//   - eshu_dp_supply_chain_impact_active_evidence_foreign_key_guard_rows_total
//     — rows the foreign-key guard statement returned; nonzero means a payload
//     contract drifted to carry an identity key its branch does not probe.
//
// The branch label is the closed factimpact.Branches() name set plus
// scope_resolution, payload_page, and identity.
const (
	// MetricSupplyChainImpactActiveEvidenceBranchSeconds is the per-statement
	// duration histogram name.
	MetricSupplyChainImpactActiveEvidenceBranchSeconds = "eshu_dp_supply_chain_impact_active_evidence_branch_seconds"
	// MetricSupplyChainImpactActiveEvidenceBranchRows is the per-statement
	// row counter name.
	MetricSupplyChainImpactActiveEvidenceBranchRows = "eshu_dp_supply_chain_impact_active_evidence_branch_rows_total"
	// MetricSupplyChainImpactActiveEvidenceGuardRows is the foreign-key guard
	// row counter name.
	MetricSupplyChainImpactActiveEvidenceGuardRows = "eshu_dp_supply_chain_impact_active_evidence_foreign_key_guard_rows_total"
	// MetricDimensionBranch labels the active-evidence instruments with the
	// closed statement label set.
	MetricDimensionBranch = "branch"
)

var vulnerabilitySuppressionMutationOutcomes = []string{
	VulnerabilitySuppressionMutationOutcomeCreated,
	VulnerabilitySuppressionMutationOutcomeUnchanged,
	VulnerabilitySuppressionMutationOutcomeRejected,
	VulnerabilitySuppressionMutationOutcomeStoreError,
}

// VulnerabilitySuppressionMutationOutcomes returns the closed, low-cardinality
// outcome vocabulary for the suppression mutation span.
func VulnerabilitySuppressionMutationOutcomes() []string {
	return append([]string(nil), vulnerabilitySuppressionMutationOutcomes...)
}
