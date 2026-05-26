package telemetry

import "slices"

const (
	// SpanQuerySBOMAttestationAttachments wraps reducer-owned SBOM and
	// attestation attachment reads from durable facts.
	SpanQuerySBOMAttestationAttachments = "query.sbom_attestation_attachments"
	// SpanQueryAdvisoryEvidence wraps source-only vulnerability advisory
	// evidence reads from active source facts.
	SpanQueryAdvisoryEvidence = "query.advisory_evidence"
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
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(
				spanNames,
				idx+1,
				SpanQueryContainerImageIdentities,
				SpanQuerySupplyChainSecurityAlerts,
				SpanQuerySBOMAttestationAttachments,
				SpanQueryAdvisoryEvidence,
				SpanQuerySupplyChainImpactFindings,
				SpanQuerySupplyChainImpactExplanation,
				SpanQuerySupplyChainImpactAggregate,
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
				SpanQuerySupplyChainImpactFindings,
				SpanQuerySupplyChainImpactExplanation,
				SpanQuerySupplyChainImpactAggregate,
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
		SpanQuerySupplyChainImpactFindings,
		SpanQuerySupplyChainImpactExplanation,
		SpanQuerySupplyChainImpactAggregate,
	)
}
