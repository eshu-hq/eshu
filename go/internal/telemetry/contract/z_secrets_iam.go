// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

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
