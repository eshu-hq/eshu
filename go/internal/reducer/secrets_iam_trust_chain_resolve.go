// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// secretsIAMExactChains walks the pre-decoded secretsIAMIndex (built by
// buildSecretsIAMIndex in secrets_iam_trust_chain_build.go) and resolves every
// exact workload-to-secret trust chain, secret access path, and posture gap.
// It is a pure function over the index so exact, partial, stale, and
// unsupported behavior can be proven without Postgres, graph, or provider
// calls.
func secretsIAMExactChains(index secretsIAMIndex) (
	[]SecretsIAMIdentityTrustChain,
	[]SecretsIAMSecretAccessPath,
	[]SecretsIAMPostureGap,
) {
	var chains []SecretsIAMIdentityTrustChain
	var paths []SecretsIAMSecretAccessPath
	var gaps []SecretsIAMPostureGap
	for serviceAccountKey, accounts := range index.serviceAccounts {
		workloads := index.workloads[serviceAccountKey]
		if len(workloads) == 0 {
			gaps = append(gaps, secretsIAMGap(
				"missing_workload_identity_use",
				SecretsIAMTrustChainStateUnresolved,
				"service account has no workload identity-use evidence",
				serviceAccountKey,
				serviceAccountFactIDs(accounts),
				[]string{"k8s_workload_identity_use"},
				nil,
			))
			continue
		}
		gcpChains, gcpPaths, gcpGaps := secretsIAMGCPExactChainsForServiceAccount(serviceAccountKey, workloads, index)
		chains = append(chains, gcpChains...)
		paths = append(paths, gcpPaths...)
		gaps = append(gaps, gcpGaps...)
		hasGCPBinding := len(index.gcpK8sBindings[serviceAccountKey]) > 0
		roles := index.irsa[serviceAccountKey]
		vaultRoles := index.vaultRoles[serviceAccountKey]
		if len(roles) == 0 || len(vaultRoles) == 0 {
			if len(gcpChains) > 0 || hasGCPBinding {
				continue
			}
			gaps = append(gaps, secretsIAMGap(
				"missing_identity_provider_hop",
				SecretsIAMTrustChainStateUnresolved,
				"service account is missing IAM role or Vault Kubernetes auth-role evidence",
				serviceAccountKey,
				serviceAccountFactIDs(accounts),
				[]string{"eks_irsa_annotation", "vault_auth_role"},
				nil,
			))
			continue
		}
		for _, roleEvidence := range roles {
			roleARN := roleEvidence.roleARN
			principals := index.iamPrincipals[roleARN]
			if len(principals) == 0 {
				gaps = append(gaps, secretsIAMGap(
					"missing_iam_principal",
					SecretsIAMTrustChainStateUnresolved,
					"IAM role principal fact is missing",
					serviceAccountKey,
					[]string{roleEvidence.env.FactID},
					[]string{"aws_iam_principal"},
					nil,
				))
				continue
			}
			trust, ok := exactIAMRoleTrust(roleEvidence, index.iamTrusts[roleARN])
			if !ok {
				gaps = append(gaps, secretsIAMGap(
					"missing_exact_iam_trust",
					SecretsIAMTrustChainStatePartial,
					"IAM role trust did not carry an exact matching IRSA subject or EKS Pod Identity service principal",
					serviceAccountKey,
					[]string{roleEvidence.env.FactID},
					[]string{"aws_iam_trust_policy"},
					nil,
				))
				continue
			}
			roleUID := secretsIAMRoleCloudResourceUID(roleARN, principals)
			for _, vaultRole := range vaultRoles {
				for _, workload := range workloads {
					chain := secretsIAMChain(serviceAccountKey, workload, roleEvidence, trust, vaultRole, roleUID)
					chains = append(chains, chain)
					chainPaths, pathGaps := secretsIAMVaultPaths(chain, vaultRole, index)
					paths = append(paths, chainPaths...)
					gaps = append(gaps, pathGaps...)
				}
			}
		}
	}
	return chains, paths, gaps
}

func exactIAMRoleTrust(roleEvidence secretsIAMIRSA, trusts []facts.Envelope) (facts.Envelope, bool) {
	if roleEvidence.env.FactKind == facts.EKSPodIdentityAssociationFactKind {
		return exactPodIdentityTrust(trusts)
	}
	return exactWebIdentityTrust(roleEvidence, trusts)
}

func exactPodIdentityTrust(trusts []facts.Envelope) (facts.Envelope, bool) {
	for _, trust := range trusts {
		if payloadString(trust.Payload, "effect") != "Allow" {
			continue
		}
		if !secretsIAMContainsLower(payloadStrings(trust.Payload, "", "actions"), "sts:assumerole") {
			continue
		}
		if secretsIAMContainsString(payloadStrings(trust.Payload, "", "assume_principals"), "pods.eks.amazonaws.com") {
			return trust, true
		}
	}
	return facts.Envelope{}, false
}

func exactWebIdentityTrust(roleEvidence secretsIAMIRSA, trusts []facts.Envelope) (facts.Envelope, bool) {
	subject := roleEvidence.webIdentitySubjectFingerprint
	if subject == "" {
		return facts.Envelope{}, false
	}
	for _, trust := range trusts {
		if payloadBool(trust.Payload, "web_identity_subject_wildcard") {
			continue
		}
		if payloadString(trust.Payload, "effect") != "Allow" {
			continue
		}
		if !secretsIAMContainsLower(payloadStrings(trust.Payload, "", "actions"), "sts:assumerolewithwebidentity") {
			continue
		}
		if secretsIAMContainsString(payloadStrings(trust.Payload, "", "web_identity_subject_fingerprints"), subject) {
			return trust, true
		}
	}
	return facts.Envelope{}, false
}

func secretsIAMChain(
	serviceAccountKey string,
	workload secretsIAMWorkload,
	roleEvidence secretsIAMIRSA,
	trust facts.Envelope,
	vaultRole secretsIAMVaultRole,
	iamRoleCloudResourceUID string,
) SecretsIAMIdentityTrustChain {
	roleFingerprint := secretsIAMFingerprint("iam_role", roleEvidence.roleARN)
	policyKeys := vaultRole.decoded.TokenPolicyJoinKeys
	workloadObjectID := stringOrEmpty(workload.decoded.WorkloadObjectID)
	evidence := []string{workload.env.FactID, roleEvidence.env.FactID, trust.FactID, vaultRole.env.FactID}
	return SecretsIAMIdentityTrustChain{
		ChainID:                 secretsIAMID("identity_trust_chain", serviceAccountKey, workloadObjectID, roleFingerprint, vaultRole.decoded.RoleJoinKey),
		State:                   SecretsIAMTrustChainStateExact,
		Confidence:              "exact",
		ServiceAccountJoinKey:   serviceAccountKey,
		WorkloadObjectID:        workloadObjectID,
		WorkloadKind:            stringOrEmpty(workload.decoded.WorkloadKind),
		IAMRoleFingerprint:      roleFingerprint,
		IAMRoleCloudResourceUID: iamRoleCloudResourceUID,
		IAMRoleAssumeMode:       secretsIAMRoleAssumeMode(roleEvidence.env.FactKind),
		VaultRoleJoinKey:        vaultRole.decoded.RoleJoinKey,
		VaultMountJoinKey:       stringOrEmpty(vaultRole.decoded.MountJoinKey),
		VaultPolicyJoinKeys:     policyKeys,
		EvidenceFactIDs:         uniqueSortedStrings(evidence),
		SourceScopes:            uniqueSortedStrings([]string{workload.env.ScopeID, roleEvidence.env.ScopeID, trust.ScopeID, vaultRole.env.ScopeID}),
		SourceGenerations:       uniqueSortedStrings([]string{workload.env.GenerationID, roleEvidence.env.GenerationID, trust.GenerationID, vaultRole.env.GenerationID}),
	}
}

func secretsIAMVaultPaths(
	chain SecretsIAMIdentityTrustChain,
	vaultRole secretsIAMVaultRole,
	index secretsIAMIndex,
) ([]SecretsIAMSecretAccessPath, []SecretsIAMPostureGap) {
	var paths []SecretsIAMSecretAccessPath
	var gaps []SecretsIAMPostureGap
	for _, policyKey := range vaultRole.decoded.TokenPolicyJoinKeys {
		policies := index.vaultPolicies[policyKey]
		if len(policies) == 0 {
			gaps = append(gaps, secretsIAMGap(
				"missing_vault_policy",
				SecretsIAMTrustChainStateUnresolved,
				"Vault auth role references a policy that was not collected",
				chain.ServiceAccountJoinKey,
				[]string{vaultRole.env.FactID},
				[]string{"vault_acl_policy"},
				nil,
			))
			continue
		}
		for _, policy := range policies {
			for _, rule := range vaultPolicyRules(policy) {
				if !secretsIAMContainsLower(rule.capabilities, "read") {
					continue
				}
				kv := index.vaultKV[rule.pathFingerprint]
				if len(kv) == 0 {
					gaps = append(gaps, secretsIAMGap(
						"missing_vault_kv_metadata",
						SecretsIAMTrustChainStateUnresolved,
						"Vault ACL policy rule has no matching KV metadata path fingerprint",
						chain.ServiceAccountJoinKey,
						[]string{policy.env.FactID},
						[]string{"vault_kv_metadata"},
						nil,
					))
					continue
				}
				for _, metadata := range kv {
					paths = append(paths, SecretsIAMSecretAccessPath{
						PathID:             secretsIAMID("secret_access_path", chain.ChainID, policyKey, rule.pathFingerprint),
						ChainID:            chain.ChainID,
						State:              SecretsIAMTrustChainStateExact,
						Confidence:         "exact",
						KVPathFingerprint:  rule.pathFingerprint,
						VaultMountJoinKey:  metadata.decoded.MountJoinKey,
						VaultPolicyJoinKey: policyKey,
						Capabilities:       uniqueSortedStrings(rule.capabilities),
						EvidenceFactIDs:    secretsIAMPathEvidence(chain, vaultRole, policy, metadata),
					})
				}
			}
		}
	}
	return paths, gaps
}

func secretsIAMPathEvidence(
	chain SecretsIAMIdentityTrustChain,
	vaultRole secretsIAMVaultRole,
	policy secretsIAMVaultPolicy,
	metadata secretsIAMVaultKV,
) []string {
	evidence := append([]string{}, chain.EvidenceFactIDs...)
	evidence = append(evidence, vaultRole.env.FactID, policy.env.FactID, metadata.env.FactID)
	return uniqueSortedStrings(evidence)
}

func secretsIAMCoverageGaps(envelopes []facts.Envelope) []SecretsIAMPostureGap {
	gaps := make([]SecretsIAMPostureGap, 0, len(envelopes))
	for _, envelope := range envelopes {
		state := secretsIAMStateFromSourceState(payloadString(envelope.Payload, "source_state"))
		gapType := "partial_source_coverage"
		if state == SecretsIAMTrustChainStateUnsupported {
			gapType = "unsupported_policy_layer"
		}
		gaps = append(gaps, secretsIAMGap(
			gapType,
			state,
			payloadString(envelope.Payload, "warning_kind"),
			"",
			[]string{envelope.FactID},
			nil,
			[]string{payloadString(envelope.Payload, "resource_scope")},
		))
	}
	return gaps
}

func secretsIAMStaleGenerationGaps(index secretsIAMIndex) []SecretsIAMPostureGap {
	var gaps []SecretsIAMPostureGap
	for serviceAccountKey, accounts := range index.serviceAccounts {
		for _, account := range accounts {
			for _, workload := range index.workloads[serviceAccountKey] {
				if account.env.ScopeID == workload.env.ScopeID && account.env.GenerationID != workload.env.GenerationID {
					gaps = append(gaps, secretsIAMGap(
						"stale_generation",
						SecretsIAMTrustChainStateStale,
						"ServiceAccount and workload identity-use evidence came from different generations",
						serviceAccountKey,
						[]string{account.env.FactID, workload.env.FactID},
						nil,
						nil,
					))
				}
			}
		}
	}
	return gaps
}
