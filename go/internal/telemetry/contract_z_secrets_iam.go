package telemetry

import "slices"

const (
	// SpanQuerySecretsIAMIdentityTrustChains wraps reducer-owned secrets/IAM
	// identity trust-chain reads from durable facts (workload to ServiceAccount
	// to IAM role to Vault policy chains). The read surface is bounded, scoped,
	// and provenance-only; it never promotes graph edges.
	SpanQuerySecretsIAMIdentityTrustChains = "query.secrets_iam_identity_trust_chains"
)

// init lands this span after the observability coverage correlation span when
// that surface is present, otherwise after Kubernetes correlations. That keeps
// the frozen read-model span order stable as the query surface grows.
func init() {
	for idx, name := range spanNames {
		if name == SpanQueryObservabilityCoverageCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQuerySecretsIAMIdentityTrustChains)
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryKubernetesCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQuerySecretsIAMIdentityTrustChains)
			return
		}
	}
	spanNames = append(spanNames, SpanQuerySecretsIAMIdentityTrustChains)
}
