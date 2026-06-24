// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

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

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryKubernetesCorrelations {
			spanNames = slices.Insert(
				spanNames,
				idx+1,
				SpanQueryContainerImageIdentities,
				SpanQuerySupplyChainSecurityAlerts,
				SpanQuerySBOMAttestationAttachments,
				SpanQueryAdvisoryEvidence,
				SpanQueryAdvisoryCatalog,
				SpanQuerySupplyChainImpactFindings,
				SpanQuerySupplyChainImpactExplanation,
				SpanQuerySupplyChainImpactAggregate,
				SpanQuerySecurityAlertReconciliationAggregate,
				SpanQueryContainerImageIdentityAggregate,
				SpanQuerySBOMAttestationAttachmentAggregate,
			)
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(
				spanNames,
				idx+1,
				SpanQueryContainerImageIdentities,
				SpanQuerySupplyChainSecurityAlerts,
				SpanQuerySBOMAttestationAttachments,
				SpanQueryAdvisoryEvidence,
				SpanQueryAdvisoryCatalog,
				SpanQuerySupplyChainImpactFindings,
				SpanQuerySupplyChainImpactExplanation,
				SpanQuerySupplyChainImpactAggregate,
				SpanQuerySecurityAlertReconciliationAggregate,
				SpanQueryContainerImageIdentityAggregate,
				SpanQuerySBOMAttestationAttachmentAggregate,
			)
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryCICDRunCorrelations {
			spanNames = slices.Insert(
				spanNames,
				idx+1,
				SpanQueryContainerImageIdentities,
				SpanQuerySupplyChainSecurityAlerts,
				SpanQuerySBOMAttestationAttachments,
				SpanQueryAdvisoryEvidence,
				SpanQueryAdvisoryCatalog,
				SpanQuerySupplyChainImpactFindings,
				SpanQuerySupplyChainImpactExplanation,
				SpanQuerySupplyChainImpactAggregate,
				SpanQuerySecurityAlertReconciliationAggregate,
				SpanQueryContainerImageIdentityAggregate,
				SpanQuerySBOMAttestationAttachmentAggregate,
			)
			return
		}
	}
	spanNames = append(
		spanNames,
		SpanQueryContainerImageIdentities,
		SpanQuerySupplyChainSecurityAlerts,
		SpanQuerySBOMAttestationAttachments,
		SpanQueryAdvisoryEvidence,
		SpanQueryAdvisoryCatalog,
		SpanQuerySupplyChainImpactFindings,
		SpanQuerySupplyChainImpactExplanation,
		SpanQuerySupplyChainImpactAggregate,
		SpanQuerySecurityAlertReconciliationAggregate,
		SpanQueryContainerImageIdentityAggregate,
		SpanQuerySBOMAttestationAttachmentAggregate,
	)
}
