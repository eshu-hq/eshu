package telemetry

import "slices"

const (
	// SpanQuerySBOMAttestationAttachments wraps reducer-owned SBOM and
	// attestation attachment reads from durable facts.
	SpanQuerySBOMAttestationAttachments = "query.sbom_attestation_attachments"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryCICDRunCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQuerySBOMAttestationAttachments)
			return
		}
	}
	spanNames = append(spanNames, SpanQuerySBOMAttestationAttachments)
}
