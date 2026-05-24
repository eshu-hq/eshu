package telemetry

import "slices"

const (
	// SpanQuerySBOMAttestationAttachments wraps reducer-owned SBOM and
	// attestation attachment reads from durable facts.
	SpanQuerySBOMAttestationAttachments = "query.sbom_attestation_attachments"
	// SpanQuerySupplyChainImpactFindings wraps reducer-owned vulnerability
	// impact finding reads from durable facts.
	SpanQuerySupplyChainImpactFindings = "query.supply_chain_impact_findings"
	// SpanQuerySupplyChainImpactExplanation wraps one bounded vulnerability
	// impact explanation over reducer-owned facts and referenced evidence ids.
	SpanQuerySupplyChainImpactExplanation = "query.supply_chain_impact_explanation"
	// SpanQueryContainerImageIdentities wraps reducer-owned image identity
	// reads from durable facts.
	SpanQueryContainerImageIdentities = "query.container_image_identities"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(
				spanNames,
				idx+1,
				SpanQueryContainerImageIdentities,
				SpanQuerySBOMAttestationAttachments,
				SpanQuerySupplyChainImpactFindings,
				SpanQuerySupplyChainImpactExplanation,
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
				SpanQuerySBOMAttestationAttachments,
				SpanQuerySupplyChainImpactFindings,
				SpanQuerySupplyChainImpactExplanation,
			)
			return
		}
	}
	spanNames = append(
		spanNames,
		SpanQueryContainerImageIdentities,
		SpanQuerySBOMAttestationAttachments,
		SpanQuerySupplyChainImpactFindings,
		SpanQuerySupplyChainImpactExplanation,
	)
}
