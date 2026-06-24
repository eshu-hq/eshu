// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

const (
	// SpanQuerySecretsIAMIdentityTrustChains wraps reducer-owned secrets/IAM
	// identity trust-chain reads from durable facts (workload to ServiceAccount
	// to IAM role to Vault policy chains). The read surface is bounded, scoped,
	// and provenance-only; it never promotes graph edges.
	SpanQuerySecretsIAMIdentityTrustChains = "query.secrets_iam_identity_trust_chains"
	// SpanQuerySecretsIAMPrivilegePostureObservations wraps reducer-owned
	// privilege posture observation reads (broad or partial posture evidence
	// that must stay provenance-only).
	SpanQuerySecretsIAMPrivilegePostureObservations = "query.secrets_iam_privilege_posture_observations"
	// SpanQuerySecretsIAMSecretAccessPaths wraps reducer-owned secret access
	// path reads (Vault policy to KV metadata paths reachable from exact
	// identity chains).
	SpanQuerySecretsIAMSecretAccessPaths = "query.secrets_iam_secret_access_paths"
	// SpanQuerySecretsIAMPostureGaps wraps reducer-owned posture gap reads
	// (missing, stale, hidden, or unsupported evidence blocking exact truth).
	SpanQuerySecretsIAMPostureGaps = "query.secrets_iam_posture_gaps"
	// SpanQuerySecretsIAMPostureSummary wraps the bounded posture summary rollup
	// (grouped counts over the secrets/IAM read models for one scope).
	SpanQuerySecretsIAMPostureSummary = "query.secrets_iam_posture_summary"
)

// secretsIAMQuerySpans is the frozen-order set of secrets/IAM query spans, with
// the identity trust-chain span first and the summary rollup last.
var secretsIAMQuerySpans = []string{
	SpanQuerySecretsIAMIdentityTrustChains,
	SpanQuerySecretsIAMPrivilegePostureObservations,
	SpanQuerySecretsIAMSecretAccessPaths,
	SpanQuerySecretsIAMPostureGaps,
	SpanQuerySecretsIAMPostureSummary,
}

// init lands the secrets/IAM query spans after the observability coverage
// correlation span when that surface is present, otherwise after Kubernetes
// correlations. That keeps the frozen read-model span order stable as the query
// surface grows.
func init() {
	for idx, name := range spanNames {
		if name == SpanQueryObservabilityCoverageCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, secretsIAMQuerySpans...)
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryKubernetesCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, secretsIAMQuerySpans...)
			return
		}
	}
	spanNames = append(spanNames, secretsIAMQuerySpans...)
}
