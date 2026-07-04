// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// secretsIAMPrincipal pairs an aws_iam_principal envelope with its decoded typed
// payload so the trust-chain build decodes each principal exactly once, at index
// build time, and quarantines a malformed one there rather than re-decoding (and
// re-failing) per role in secretsIAMRoleCloudResourceUID.
type secretsIAMPrincipal struct {
	env     facts.Envelope
	decoded iamv1.Principal
}

type secretsIAMIndex struct {
	serviceAccounts map[string][]facts.Envelope
	workloads       map[string][]facts.Envelope
	irsa            map[string][]facts.Envelope
	vaultRoles      map[string][]facts.Envelope
	vaultAuthRoles  []facts.Envelope
	// iamPrincipals holds only the aws_iam_principal facts that decoded cleanly,
	// keyed by principal_arn. A malformed principal is quarantined during
	// buildSecretsIAMIndex and never enters the index, so the trust-chain build
	// reads only valid principals.
	iamPrincipals  map[string][]secretsIAMPrincipal
	iamTrusts      map[string][]facts.Envelope
	vaultPolicies  map[string][]facts.Envelope
	vaultKV        map[string][]facts.Envelope
	gcpPrincipals  map[string][]facts.Envelope
	gcpTrusts      map[string][]facts.Envelope
	gcpK8sBindings map[string][]facts.Envelope
	gcpPermissions map[string][]facts.Envelope
	coverage       []facts.Envelope
}

// BuildSecretsIAMTrustChainReadModels builds reducer-owned secrets/IAM read
// models from redacted source facts. It is pure so exact, partial, stale, and
// unsupported behavior can be proven without Postgres, graph, or provider calls.
//
// A malformed aws_iam_principal fact whose payload is missing a required
// identity field (an input_invalid decode failure) is quarantined per-fact and
// returned in the []quarantinedFact slice: it is skipped so the valid trust
// chains (including the untouched K8s/GCP/Vault chains, which never decode an
// aws_iam_principal) still project, matching the per-fact isolation contract
// every other migrated reducer kind follows. The malformed principal simply
// never resolves an IAM-role CloudResource uid, so no chain resolves against a
// zero-value identity.
//
// Any OTHER decode error (a non-input_invalid classification the seam may add
// later, or a non-*factDecodeError) is FATAL and returned as the error: it fails
// the whole work item so the durable queue triages it, rather than being
// swallowed as a silent success.
func BuildSecretsIAMTrustChainReadModels(envelopes []facts.Envelope) (SecretsIAMTrustChainReadModels, []quarantinedFact, error) {
	index, quarantined, err := buildSecretsIAMIndex(envelopes)
	if err != nil {
		return SecretsIAMTrustChainReadModels{}, nil, err
	}
	models := SecretsIAMTrustChainReadModels{}
	models.PostureGaps = append(models.PostureGaps, secretsIAMCoverageGaps(index.coverage)...)
	models.PostureGaps = append(models.PostureGaps, secretsIAMStaleGenerationGaps(index)...)
	models.PrivilegePostureObservations = append(
		models.PrivilegePostureObservations,
		secretsIAMWildcardTrustObservations(index.iamTrusts)...,
	)
	models.PrivilegePostureObservations = append(
		models.PrivilegePostureObservations,
		secretsIAMWildcardVaultAuthRoleObservations(index.vaultAuthRoles)...,
	)
	models.PrivilegePostureObservations = append(
		models.PrivilegePostureObservations,
		secretsIAMExternalTrustObservations(index.iamTrusts)...,
	)
	models.PrivilegePostureObservations = append(
		models.PrivilegePostureObservations,
		secretsIAMGCPGrantObservations(index)...,
	)
	chains, paths, gaps := secretsIAMExactChains(index)
	models.IdentityTrustChains = append(models.IdentityTrustChains, chains...)
	models.SecretAccessPaths = append(models.SecretAccessPaths, paths...)
	models.PostureGaps = append(models.PostureGaps, gaps...)
	sortSecretsIAMReadModels(&models)
	return models, quarantined, nil
}

func buildSecretsIAMIndex(envelopes []facts.Envelope) (secretsIAMIndex, []quarantinedFact, error) {
	index := secretsIAMIndex{
		serviceAccounts: map[string][]facts.Envelope{},
		workloads:       map[string][]facts.Envelope{},
		irsa:            map[string][]facts.Envelope{},
		vaultRoles:      map[string][]facts.Envelope{},
		iamPrincipals:   map[string][]secretsIAMPrincipal{},
		iamTrusts:       map[string][]facts.Envelope{},
		vaultPolicies:   map[string][]facts.Envelope{},
		vaultKV:         map[string][]facts.Envelope{},
		gcpPrincipals:   map[string][]facts.Envelope{},
		gcpTrusts:       map[string][]facts.Envelope{},
		gcpK8sBindings:  map[string][]facts.Envelope{},
		gcpPermissions:  map[string][]facts.Envelope{},
	}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		switch envelope.FactKind {
		case facts.KubernetesServiceAccountFactKind:
			addByKey(index.serviceAccounts, payloadString(envelope.Payload, "service_account_join_key"), envelope)
		case facts.KubernetesWorkloadIdentityUseFactKind:
			addByKey(index.workloads, payloadString(envelope.Payload, "service_account_join_key"), envelope)
		case facts.EKSIRSAAnnotationFactKind, facts.EKSPodIdentityAssociationFactKind:
			addByKey(index.irsa, payloadString(envelope.Payload, "service_account_join_key"), envelope)
		case facts.VaultAuthRoleFactKind:
			index.vaultAuthRoles = append(index.vaultAuthRoles, envelope)
			if payloadBool(envelope.Payload, "bound_service_account_selector_wildcard") {
				continue
			}
			for _, key := range payloadStrings(envelope.Payload, "", "bound_service_account_join_keys") {
				addByKey(index.vaultRoles, key, envelope)
			}
		case facts.AWSIAMPrincipalFactKind:
			principal, err := decodeAWSIAMPrincipal(envelope)
			if err != nil {
				q, ok, fatal := partitionDecodeFailures(envelope, err)
				if fatal != nil {
					// MANDATORY fatal passthrough: partitionDecodeFailures returns a
					// non-nil fatal for any decode error it did NOT classify
					// input_invalid (a future schema-mismatch/unsupported-major class,
					// or a non-*factDecodeError). Such an error is terminal but is NOT
					// a per-fact quarantine — it must fail the whole trust-chain work
					// item so the durable queue triages it, exactly like the other 29
					// decode call sites `return ..., fatal`. Swallowing it here (the
					// old `continue`) would let a malformed principal Ack as success —
					// the "swallow failures" hole the redesign exists to close.
					return secretsIAMIndex{}, nil, fatal
				}
				if ok {
					quarantined = append(quarantined, q)
				}
				continue
			}
			key := strings.TrimSpace(payloadString(envelope.Payload, "principal_arn"))
			if key == "" {
				continue
			}
			index.iamPrincipals[key] = append(index.iamPrincipals[key], secretsIAMPrincipal{env: envelope, decoded: principal})
		case facts.AWSIAMTrustPolicyFactKind:
			addByKey(index.iamTrusts, payloadString(envelope.Payload, "role_arn"), envelope)
		case facts.VaultACLPolicyFactKind:
			addByKey(index.vaultPolicies, payloadString(envelope.Payload, "policy_join_key"), envelope)
		case facts.VaultKVMetadataFactKind:
			addByKey(index.vaultKV, payloadString(envelope.Payload, "kv_path_fingerprint"), envelope)
		case facts.GCPIAMPrincipalFactKind:
			addByKey(index.gcpPrincipals, payloadString(envelope.Payload, "principal_fingerprint"), envelope)
		case facts.GCPIAMTrustPolicyFactKind:
			addByKey(index.gcpTrusts, payloadString(envelope.Payload, "target_service_account_email_digest"), envelope)
		case facts.KubernetesGCPWorkloadIdentityBindingFactKind:
			addByKey(index.gcpK8sBindings, payloadString(envelope.Payload, "service_account_join_key"), envelope)
		case facts.GCPIAMPermissionPolicyFactKind:
			addByKey(index.gcpPermissions, payloadString(envelope.Payload, "principal_fingerprint"), envelope)
		case facts.SecretsIAMCoverageWarningFactKind:
			index.coverage = append(index.coverage, envelope)
		}
	}
	return index, quarantined, nil
}

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
				factIDs(accounts),
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
				factIDs(accounts),
				[]string{"eks_irsa_annotation", "vault_auth_role"},
				nil,
			))
			continue
		}
		for _, roleEvidence := range roles {
			roleARN := payloadString(roleEvidence.Payload, "role_arn")
			principals := index.iamPrincipals[roleARN]
			if len(principals) == 0 {
				gaps = append(gaps, secretsIAMGap(
					"missing_iam_principal",
					SecretsIAMTrustChainStateUnresolved,
					"IAM role principal fact is missing",
					serviceAccountKey,
					[]string{roleEvidence.FactID},
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
					[]string{roleEvidence.FactID},
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

func exactIAMRoleTrust(roleEvidence facts.Envelope, trusts []facts.Envelope) (facts.Envelope, bool) {
	if roleEvidence.FactKind == facts.EKSPodIdentityAssociationFactKind {
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

func exactWebIdentityTrust(roleEvidence facts.Envelope, trusts []facts.Envelope) (facts.Envelope, bool) {
	subject := payloadString(roleEvidence.Payload, "web_identity_subject_fingerprint")
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
	workload facts.Envelope,
	roleEvidence facts.Envelope,
	trust facts.Envelope,
	vaultRole facts.Envelope,
	iamRoleCloudResourceUID string,
) SecretsIAMIdentityTrustChain {
	roleFingerprint := secretsIAMFingerprint("iam_role", payloadString(roleEvidence.Payload, "role_arn"))
	policyKeys := payloadStrings(vaultRole.Payload, "", "token_policy_join_keys")
	evidence := []string{workload.FactID, roleEvidence.FactID, trust.FactID, vaultRole.FactID}
	return SecretsIAMIdentityTrustChain{
		ChainID:                 secretsIAMID("identity_trust_chain", serviceAccountKey, payloadString(workload.Payload, "workload_object_id"), roleFingerprint, payloadString(vaultRole.Payload, "role_join_key")),
		State:                   SecretsIAMTrustChainStateExact,
		Confidence:              "exact",
		ServiceAccountJoinKey:   serviceAccountKey,
		WorkloadObjectID:        payloadString(workload.Payload, "workload_object_id"),
		WorkloadKind:            payloadString(workload.Payload, "workload_kind"),
		IAMRoleFingerprint:      roleFingerprint,
		IAMRoleCloudResourceUID: iamRoleCloudResourceUID,
		IAMRoleAssumeMode:       secretsIAMRoleAssumeMode(roleEvidence.FactKind),
		VaultRoleJoinKey:        payloadString(vaultRole.Payload, "role_join_key"),
		VaultMountJoinKey:       payloadString(vaultRole.Payload, "mount_join_key"),
		VaultPolicyJoinKeys:     policyKeys,
		EvidenceFactIDs:         uniqueSortedStrings(evidence),
		SourceScopes:            uniqueSortedStrings([]string{workload.ScopeID, roleEvidence.ScopeID, trust.ScopeID, vaultRole.ScopeID}),
		SourceGenerations:       uniqueSortedStrings([]string{workload.GenerationID, roleEvidence.GenerationID, trust.GenerationID, vaultRole.GenerationID}),
	}
}

func secretsIAMVaultPaths(
	chain SecretsIAMIdentityTrustChain,
	vaultRole facts.Envelope,
	index secretsIAMIndex,
) ([]SecretsIAMSecretAccessPath, []SecretsIAMPostureGap) {
	var paths []SecretsIAMSecretAccessPath
	var gaps []SecretsIAMPostureGap
	for _, policyKey := range payloadStrings(vaultRole.Payload, "", "token_policy_join_keys") {
		policies := index.vaultPolicies[policyKey]
		if len(policies) == 0 {
			gaps = append(gaps, secretsIAMGap(
				"missing_vault_policy",
				SecretsIAMTrustChainStateUnresolved,
				"Vault auth role references a policy that was not collected",
				chain.ServiceAccountJoinKey,
				[]string{vaultRole.FactID},
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
						[]string{policy.FactID},
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
						VaultMountJoinKey:  payloadString(metadata.Payload, "mount_join_key"),
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
	vaultRole facts.Envelope,
	policy facts.Envelope,
	metadata facts.Envelope,
) []string {
	evidence := append([]string{}, chain.EvidenceFactIDs...)
	evidence = append(evidence, vaultRole.FactID, policy.FactID, metadata.FactID)
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
				if account.ScopeID == workload.ScopeID && account.GenerationID != workload.GenerationID {
					gaps = append(gaps, secretsIAMGap(
						"stale_generation",
						SecretsIAMTrustChainStateStale,
						"ServiceAccount and workload identity-use evidence came from different generations",
						serviceAccountKey,
						[]string{account.FactID, workload.FactID},
						nil,
						nil,
					))
				}
			}
		}
	}
	return gaps
}
