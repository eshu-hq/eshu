package telemetry

import "slices"

const (
	// SpanQuerySBOMAttestationAttachments wraps reducer-owned SBOM and
	// attestation attachment reads from durable facts.
	SpanQuerySBOMAttestationAttachments = "query.sbom_attestation_attachments"
	// SpanQuerySupplyChainImpactFindings wraps reducer-owned vulnerability
	// impact finding reads from durable facts.
	SpanQuerySupplyChainImpactFindings = "query.supply_chain_impact_findings"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryCICDRunCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQuerySBOMAttestationAttachments, SpanQuerySupplyChainImpactFindings)
			return
		}
	}
	spanNames = append(spanNames, SpanQuerySBOMAttestationAttachments, SpanQuerySupplyChainImpactFindings)
}
