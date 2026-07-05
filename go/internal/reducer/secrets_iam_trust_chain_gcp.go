// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// GCP IAM grant posture risk types, the bounded classifications the GCP IAM
// permission grants produce as secrets/IAM privilege-posture observations
// (issue #2347). They mirror how AWS wildcard trusts become posture observations
// rather than exact chains: a GCP grant is explicit evidence of standing access,
// surfaced as posture until the impersonation/Workload-Identity trust layer
// (#2369) connects a workload to the service account.
const (
	gcpRiskSecretAccessGrant = "gcp_service_account_secret_access" // #nosec G101 -- risk-type token string identifier, not a credential
	gcpRiskBroadRoleGrant    = "gcp_service_account_broad_role"    // #nosec G101 -- risk-type token string identifier, not a credential
)

// secretsIAMGCPGrantObservations projects the GCP IAM principal/permission
// source facts into secrets/IAM privilege-posture observations. A grant is
// surfaced when it carries standing privilege worth an operator's attention: an
// IAM role grant on a Secret Manager secret resource (resource_is_secret) or a
// broad primitive role (broad_role). Exact GCP secret access paths are gated
// separately on roles that include secretmanager.versions.access. A
// service-account principal with only narrow, non-secret grants is still
// consumed (indexed and joined) but yields no observation, exactly as a benign
// AWS trust yields none.
//
// The subject is the redaction-safe service-account fingerprint shared by the
// principal and permission facts, so the observation never leaks a raw member
// identity. Each observation requires a matching principal fact for the grant's
// fingerprint, so an orphan permission fact does not fabricate an identity.
func secretsIAMGCPGrantObservations(index secretsIAMIndex) []SecretsIAMPrivilegePostureObservation {
	if len(index.gcpPermissions) == 0 {
		return nil
	}

	fingerprints := make([]string, 0, len(index.gcpPermissions))
	for fingerprint := range index.gcpPermissions {
		fingerprints = append(fingerprints, fingerprint)
	}
	sort.Strings(fingerprints)

	var observations []SecretsIAMPrivilegePostureObservation
	for _, fingerprint := range fingerprints {
		if len(index.gcpPrincipals[fingerprint]) == 0 {
			// No principal fact for this grant's identity: do not invent an
			// identity from a permission fact alone.
			continue
		}
		principalFactID := index.gcpPrincipals[fingerprint][0].FactID
		for _, grant := range index.gcpPermissions[fingerprint] {
			riskType, ok := gcpGrantRiskType(grant)
			if !ok {
				continue
			}
			observations = append(observations, SecretsIAMPrivilegePostureObservation{
				ObservationID: secretsIAMID(
					"privilege_posture_observation",
					riskType,
					fingerprint,
					payloadString(grant.Payload, "role"),
					grant.FactID,
				),
				RiskType:           riskType,
				Severity:           "high",
				State:              SecretsIAMTrustChainStateExact,
				Confidence:         "exact",
				SubjectFingerprint: fingerprint,
				Reason:             gcpGrantReason(riskType),
				EvidenceFactIDs:    uniqueSortedStrings([]string{principalFactID, grant.FactID}),
			})
		}
	}
	return observations
}

func secretsIAMGCPExactChainsForServiceAccount(
	serviceAccountKey string,
	workloads []secretsIAMWorkload,
	index secretsIAMIndex,
) ([]SecretsIAMIdentityTrustChain, []SecretsIAMSecretAccessPath, []SecretsIAMPostureGap) {
	// index.gcpK8sBindings holds decoded k8s_gcp_workload_identity_binding
	// facts (secretsIAMGCPBinding): this K8S-lane kind IS in scope for Wave
	// 4d. Only the downstream join against gcp_iam_trust_policy stays on raw
	// payloadString reads below -- deferred: gcp_iam lane, Wave 4d types
	// vault/k8s only.
	bindings := index.gcpK8sBindings[serviceAccountKey]
	if len(bindings) == 0 {
		return nil, nil, nil
	}
	var chains []SecretsIAMIdentityTrustChain
	var paths []SecretsIAMSecretAccessPath
	var gaps []SecretsIAMPostureGap
	for _, binding := range bindings {
		emailDigest := binding.decoded.GCPServiceAccountEmailDigest
		subject := binding.decoded.GCPWorkloadIdentitySubjectFingerprint
		// exactGCPWorkloadIdentityTrusts reads gcp_iam_trust_policy facts raw.
		// deferred: gcp_iam lane, Wave 4d types vault/k8s only.
		trusts := exactGCPWorkloadIdentityTrusts(emailDigest, subject, index.gcpTrusts[emailDigest])
		if len(trusts) == 0 {
			gaps = append(gaps, secretsIAMGap(
				"missing_gcp_workload_identity_trust",
				SecretsIAMTrustChainStatePartial,
				"GCP service-account trust did not carry an exact matching Workload Identity subject",
				serviceAccountKey,
				[]string{binding.env.FactID},
				[]string{"gcp_iam_trust_policy"},
				nil,
			))
			continue
		}
		for _, trust := range trusts {
			// deferred: gcp_iam lane, Wave 4d types vault/k8s only.
			targetFingerprint := payloadString(trust.Payload, "target_principal_fingerprint")
			principals := index.gcpPrincipals[targetFingerprint]
			if len(principals) == 0 {
				gaps = append(gaps, secretsIAMGap(
					"missing_gcp_principal",
					SecretsIAMTrustChainStateUnresolved,
					"GCP service-account principal fact is missing",
					serviceAccountKey,
					[]string{binding.env.FactID, trust.FactID},
					[]string{"gcp_iam_principal"},
					nil,
				))
				continue
			}
			for _, workload := range workloads {
				chain := secretsIAMGCPChain(serviceAccountKey, workload, binding, trust, principals[0])
				chains = append(chains, chain)
				paths = append(paths, secretsIAMGCPSecretAccessPaths(chain, index.gcpPermissions[targetFingerprint])...)
			}
		}
	}
	return chains, paths, gaps
}

// exactGCPWorkloadIdentityTrusts reads gcp_iam_trust_policy facts raw.
// deferred: gcp_iam lane, Wave 4d types vault/k8s only.
func exactGCPWorkloadIdentityTrusts(
	emailDigest string,
	subjectFingerprint string,
	trusts []facts.Envelope,
) []facts.Envelope {
	if emailDigest == "" || subjectFingerprint == "" {
		return nil
	}
	var out []facts.Envelope
	for _, trust := range trusts {
		if payloadString(trust.Payload, "target_service_account_email_digest") != emailDigest {
			continue
		}
		if payloadString(trust.Payload, "gcp_workload_identity_subject_fingerprint") != subjectFingerprint {
			continue
		}
		if payloadString(trust.Payload, "impersonation_mode") != "workload_identity" {
			continue
		}
		if payloadString(trust.Payload, "role") != "roles/iam.workloadIdentityUser" {
			continue
		}
		out = append(out, trust)
	}
	return out
}

func secretsIAMGCPChain(
	serviceAccountKey string,
	workload secretsIAMWorkload,
	binding secretsIAMGCPBinding,
	trust facts.Envelope,
	principal facts.Envelope,
) SecretsIAMIdentityTrustChain {
	targetFingerprint := payloadString(trust.Payload, "target_principal_fingerprint")
	workloadObjectID := stringOrEmpty(workload.decoded.WorkloadObjectID)
	evidence := []string{workload.env.FactID, binding.env.FactID, trust.FactID, principal.FactID}
	return SecretsIAMIdentityTrustChain{
		ChainID: secretsIAMID(
			"identity_trust_chain",
			"gcp",
			serviceAccountKey,
			workloadObjectID,
			targetFingerprint,
			trust.FactID,
		),
		State:                             SecretsIAMTrustChainStateExact,
		Confidence:                        "exact",
		ServiceAccountJoinKey:             serviceAccountKey,
		WorkloadObjectID:                  workloadObjectID,
		WorkloadKind:                      stringOrEmpty(workload.decoded.WorkloadKind),
		GCPServiceAccountFingerprint:      targetFingerprint,
		GCPServiceAccountCloudResourceUID: payloadString(trust.Payload, "target_service_account_cloud_resource_uid"),
		GCPServiceAccountAssumeMode:       payloadString(trust.Payload, "impersonation_mode"),
		EvidenceFactIDs:                   uniqueSortedStrings(evidence),
		SourceScopes:                      uniqueSortedStrings([]string{workload.env.ScopeID, binding.env.ScopeID, trust.ScopeID, principal.ScopeID}),
		SourceGenerations:                 uniqueSortedStrings([]string{workload.env.GenerationID, binding.env.GenerationID, trust.GenerationID, principal.GenerationID}),
	}
}

func secretsIAMGCPSecretAccessPaths(
	chain SecretsIAMIdentityTrustChain,
	permissions []facts.Envelope,
) []SecretsIAMSecretAccessPath {
	var paths []SecretsIAMSecretAccessPath
	for _, permission := range permissions {
		if !payloadBool(permission.Payload, "resource_is_secret") {
			continue
		}
		capabilities := gcpSecretAccessCapabilities(permission)
		if len(capabilities) == 0 {
			continue
		}
		resource := payloadString(permission.Payload, "resource_full_resource_name")
		resourceFingerprint := secretsIAMFingerprint("gcp_secret_resource", resource)
		if resourceFingerprint == "" {
			continue
		}
		evidence := append([]string{}, chain.EvidenceFactIDs...)
		evidence = append(evidence, permission.FactID)
		paths = append(paths, SecretsIAMSecretAccessPath{
			PathID: secretsIAMID(
				"secret_access_path",
				"gcp",
				chain.ChainID,
				resourceFingerprint,
				payloadString(permission.Payload, "role"),
			),
			ChainID:                        chain.ChainID,
			State:                          SecretsIAMTrustChainStateExact,
			Confidence:                     "exact",
			CloudProvider:                  "gcp",
			CloudSecretResourceFingerprint: resourceFingerprint,
			Capabilities:                   capabilities,
			EvidenceFactIDs:                uniqueSortedStrings(evidence),
		})
	}
	return paths
}

func gcpSecretAccessCapabilities(permission facts.Envelope) []string {
	role := strings.TrimSpace(payloadString(permission.Payload, "role"))
	switch role {
	case "roles/owner", "roles/secretmanager.admin", "roles/secretmanager.secretAccessor":
		return []string{"secretmanager.versions.access"}
	default:
		return nil
	}
}

// gcpGrantRiskType classifies one GCP permission grant into a bounded posture
// risk type, preferring the secret-access classification when a broad role is
// also granted directly on a secret resource.
func gcpGrantRiskType(grant facts.Envelope) (string, bool) {
	if payloadBool(grant.Payload, "resource_is_secret") {
		return gcpRiskSecretAccessGrant, true
	}
	if payloadBool(grant.Payload, "broad_role") {
		return gcpRiskBroadRoleGrant, true
	}
	return "", false
}

func gcpGrantReason(riskType string) string {
	switch riskType {
	case gcpRiskSecretAccessGrant:
		return "GCP service account has a direct IAM role grant on a Secret Manager secret resource"
	case gcpRiskBroadRoleGrant:
		return "GCP service account holds a broad primitive role (owner/editor)"
	default:
		return "GCP service account IAM grant"
	}
}
