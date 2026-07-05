// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

// decodeVaultAuthRole decodes one vault_auth_role envelope into the typed
// secretsiamv1.VaultAuthRole struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// role_join_key field or is otherwise malformed. It is the single decode
// site for the vault_auth_role kind on the reducer side: buildSecretsIAMIndex
// decodes every vault_auth_role fact through here, and a missing required
// field is routed through partitionDecodeFailures so it dead-letters as a
// per-fact input_invalid quarantine rather than the fact silently vanishing
// from index.vaultRoles/vaultAuthRoles under addByKey's pre-typing
// blank-key guard.
func decodeVaultAuthRole(env facts.Envelope) (secretsiamv1.VaultAuthRole, error) {
	role, err := factschema.DecodeVaultAuthRole(factschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.VaultAuthRole{}, newFactDecodeError(factschema.FactKindVaultAuthRole, err)
	}
	return role, nil
}

// decodeVaultACLPolicy decodes one vault_acl_policy envelope into the typed
// secretsiamv1.VaultACLPolicy struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// policy_join_key field or is otherwise malformed. It is the single decode
// site for the vault_acl_policy kind on the reducer side.
func decodeVaultACLPolicy(env facts.Envelope) (secretsiamv1.VaultACLPolicy, error) {
	policy, err := factschema.DecodeVaultACLPolicy(factschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.VaultACLPolicy{}, newFactDecodeError(factschema.FactKindVaultACLPolicy, err)
	}
	return policy, nil
}

// decodeVaultKVMetadata decodes one vault_kv_metadata envelope into the typed
// secretsiamv1.VaultKVMetadata struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// identity field (mount_join_key, kv_path_fingerprint) or is otherwise
// malformed. It is the single decode site for the vault_kv_metadata kind on
// the reducer side.
func decodeVaultKVMetadata(env facts.Envelope) (secretsiamv1.VaultKVMetadata, error) {
	metadata, err := factschema.DecodeVaultKVMetadata(factschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.VaultKVMetadata{}, newFactDecodeError(factschema.FactKindVaultKVMetadata, err)
	}
	return metadata, nil
}

// decodeKubernetesServiceAccount decodes one k8s_service_account envelope
// into the typed secretsiamv1.KubernetesServiceAccount struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required service_account_join_key field or is
// otherwise malformed. It is the single decode site for the
// k8s_service_account kind on the reducer side.
func decodeKubernetesServiceAccount(env facts.Envelope) (secretsiamv1.KubernetesServiceAccount, error) {
	account, err := factschema.DecodeKubernetesServiceAccount(factschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.KubernetesServiceAccount{}, newFactDecodeError(factschema.FactKindKubernetesServiceAccount, err)
	}
	return account, nil
}

// decodeKubernetesWorkloadIdentityUse decodes one k8s_workload_identity_use
// envelope into the typed secretsiamv1.KubernetesWorkloadIdentityUse struct
// through the contracts seam, returning a self-classifying *factDecodeError
// when the payload is missing its required service_account_join_key field or
// is otherwise malformed. It is the single decode site for the
// k8s_workload_identity_use kind on the reducer side.
func decodeKubernetesWorkloadIdentityUse(env facts.Envelope) (secretsiamv1.KubernetesWorkloadIdentityUse, error) {
	use, err := factschema.DecodeKubernetesWorkloadIdentityUse(factschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.KubernetesWorkloadIdentityUse{}, newFactDecodeError(factschema.FactKindKubernetesWorkloadIdentityUse, err)
	}
	return use, nil
}

// decodeEKSIRSAAnnotation decodes one eks_irsa_annotation envelope into the
// typed secretsiamv1.EKSIRSAAnnotation struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// a required identity field (service_account_join_key, role_arn) or is
// otherwise malformed. It is the single decode site for the
// eks_irsa_annotation kind on the reducer side.
func decodeEKSIRSAAnnotation(env facts.Envelope) (secretsiamv1.EKSIRSAAnnotation, error) {
	annotation, err := factschema.DecodeEKSIRSAAnnotation(factschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.EKSIRSAAnnotation{}, newFactDecodeError(factschema.FactKindEKSIRSAAnnotation, err)
	}
	return annotation, nil
}

// decodeEKSPodIdentityAssociation decodes one eks_pod_identity_association
// envelope into the typed secretsiamv1.EKSPodIdentityAssociation struct
// through the contracts seam, returning a self-classifying *factDecodeError
// when the payload is missing a required identity field
// (service_account_join_key, role_arn) or is otherwise malformed. It is the
// single decode site for the eks_pod_identity_association kind on the
// reducer side.
func decodeEKSPodIdentityAssociation(env facts.Envelope) (secretsiamv1.EKSPodIdentityAssociation, error) {
	association, err := factschema.DecodeEKSPodIdentityAssociation(factschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.EKSPodIdentityAssociation{}, newFactDecodeError(factschema.FactKindEKSPodIdentityAssociation, err)
	}
	return association, nil
}

// decodeKubernetesGCPWorkloadIdentityBinding decodes one
// k8s_gcp_workload_identity_binding envelope into the typed
// secretsiamv1.KubernetesGCPWorkloadIdentityBinding struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing a required identity field (service_account_join_key,
// gcp_service_account_email_digest,
// gcp_workload_identity_subject_fingerprint) or is otherwise malformed. It
// is the single decode site for the k8s_gcp_workload_identity_binding kind
// on the reducer side. This kind is IN SCOPE for Wave 4d (the K8S lane)
// even though it is read in secrets_iam_trust_chain_gcp.go alongside the
// deferred gcp_iam lane's gcp_iam_trust_policy read: this fact is the
// Kubernetes-side annotation, not a GCP IAM lane kind.
func decodeKubernetesGCPWorkloadIdentityBinding(env facts.Envelope) (secretsiamv1.KubernetesGCPWorkloadIdentityBinding, error) {
	binding, err := factschema.DecodeKubernetesGCPWorkloadIdentityBinding(factschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.KubernetesGCPWorkloadIdentityBinding{}, newFactDecodeError(factschema.FactKindKubernetesGCPWorkloadIdentityBinding, err)
	}
	return binding, nil
}
