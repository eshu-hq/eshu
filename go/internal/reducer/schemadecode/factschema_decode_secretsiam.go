// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

// DecodeVaultAuthRole decodes one vault_auth_role envelope into the typed
// secretsiamv1.VaultAuthRole struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// role_join_key field or is otherwise malformed. It is the single decode
// site for the vault_auth_role kind on the reducer side: buildSecretsIAMIndex
// decodes every vault_auth_role fact through here, and a missing required
// field is routed through partitionDecodeFailures so it dead-letters as a
// per-fact input_invalid quarantine rather than the fact silently vanishing
// from index.vaultRoles/vaultAuthRoles under addByKey's pre-typing
// blank-key guard.
func DecodeVaultAuthRole(env facts.Envelope) (secretsiamv1.VaultAuthRole, error) {
	role, err := factschema.DecodeVaultAuthRole(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.VaultAuthRole{}, factdecode.NewFactDecodeError(factschema.FactKindVaultAuthRole, err)
	}
	return role, nil
}

// DecodeVaultACLPolicy decodes one vault_acl_policy envelope into the typed
// secretsiamv1.VaultACLPolicy struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// policy_join_key field or is otherwise malformed. It is the single decode
// site for the vault_acl_policy kind on the reducer side.
func DecodeVaultACLPolicy(env facts.Envelope) (secretsiamv1.VaultACLPolicy, error) {
	policy, err := factschema.DecodeVaultACLPolicy(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.VaultACLPolicy{}, factdecode.NewFactDecodeError(factschema.FactKindVaultACLPolicy, err)
	}
	return policy, nil
}

// DecodeVaultKVMetadata decodes one vault_kv_metadata envelope into the typed
// secretsiamv1.VaultKVMetadata struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// identity field (mount_join_key, kv_path_fingerprint) or is otherwise
// malformed. It is the single decode site for the vault_kv_metadata kind on
// the reducer side.
func DecodeVaultKVMetadata(env facts.Envelope) (secretsiamv1.VaultKVMetadata, error) {
	metadata, err := factschema.DecodeVaultKVMetadata(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.VaultKVMetadata{}, factdecode.NewFactDecodeError(factschema.FactKindVaultKVMetadata, err)
	}
	return metadata, nil
}

// DecodeKubernetesServiceAccount decodes one k8s_service_account envelope
// into the typed secretsiamv1.KubernetesServiceAccount struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required service_account_join_key field or is
// otherwise malformed. It is the single decode site for the
// k8s_service_account kind on the reducer side.
func DecodeKubernetesServiceAccount(env facts.Envelope) (secretsiamv1.KubernetesServiceAccount, error) {
	account, err := factschema.DecodeKubernetesServiceAccount(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.KubernetesServiceAccount{}, factdecode.NewFactDecodeError(factschema.FactKindKubernetesServiceAccount, err)
	}
	return account, nil
}

// DecodeKubernetesWorkloadIdentityUse decodes one k8s_workload_identity_use
// envelope into the typed secretsiamv1.KubernetesWorkloadIdentityUse struct
// through the contracts seam, returning a self-classifying *factDecodeError
// when the payload is missing its required service_account_join_key field or
// is otherwise malformed. It is the single decode site for the
// k8s_workload_identity_use kind on the reducer side.
func DecodeKubernetesWorkloadIdentityUse(env facts.Envelope) (secretsiamv1.KubernetesWorkloadIdentityUse, error) {
	use, err := factschema.DecodeKubernetesWorkloadIdentityUse(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.KubernetesWorkloadIdentityUse{}, factdecode.NewFactDecodeError(factschema.FactKindKubernetesWorkloadIdentityUse, err)
	}
	return use, nil
}

// DecodeEKSIRSAAnnotation decodes one eks_irsa_annotation envelope into the
// typed secretsiamv1.EKSIRSAAnnotation struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// a required identity field (service_account_join_key, role_arn) or is
// otherwise malformed. It is the single decode site for the
// eks_irsa_annotation kind on the reducer side.
func DecodeEKSIRSAAnnotation(env facts.Envelope) (secretsiamv1.EKSIRSAAnnotation, error) {
	annotation, err := factschema.DecodeEKSIRSAAnnotation(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.EKSIRSAAnnotation{}, factdecode.NewFactDecodeError(factschema.FactKindEKSIRSAAnnotation, err)
	}
	return annotation, nil
}

// DecodeEKSPodIdentityAssociation decodes one eks_pod_identity_association
// envelope into the typed secretsiamv1.EKSPodIdentityAssociation struct
// through the contracts seam, returning a self-classifying *factDecodeError
// when the payload is missing a required identity field
// (service_account_join_key, role_arn) or is otherwise malformed. It is the
// single decode site for the eks_pod_identity_association kind on the
// reducer side.
func DecodeEKSPodIdentityAssociation(env facts.Envelope) (secretsiamv1.EKSPodIdentityAssociation, error) {
	association, err := factschema.DecodeEKSPodIdentityAssociation(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.EKSPodIdentityAssociation{}, factdecode.NewFactDecodeError(factschema.FactKindEKSPodIdentityAssociation, err)
	}
	return association, nil
}

// DecodeKubernetesGCPWorkloadIdentityBinding decodes one
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
func DecodeKubernetesGCPWorkloadIdentityBinding(env facts.Envelope) (secretsiamv1.KubernetesGCPWorkloadIdentityBinding, error) {
	binding, err := factschema.DecodeKubernetesGCPWorkloadIdentityBinding(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.KubernetesGCPWorkloadIdentityBinding{}, factdecode.NewFactDecodeError(factschema.FactKindKubernetesGCPWorkloadIdentityBinding, err)
	}
	return binding, nil
}

// DecodeAWSIAMTrustPolicy decodes one aws_iam_trust_policy envelope into the
// typed secretsiamv1.AWSIAMTrustPolicy struct through the contracts seam,
// returning a self-classifying *factDecodeError on malformed input. The live
// decode site is the loader: go/internal/storage/postgres's trust-chain
// anchor decoder calls factschema.DecodeAWSIAMTrustPolicy directly, so this
// wrapper is the payload-usage manifest's attribution identity for those
// reads (#6392), not an additional runtime call path.
func DecodeAWSIAMTrustPolicy(env facts.Envelope) (secretsiamv1.AWSIAMTrustPolicy, error) {
	policy, err := factschema.DecodeAWSIAMTrustPolicy(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.AWSIAMTrustPolicy{}, factdecode.NewFactDecodeError(factschema.FactKindAWSIAMTrustPolicy, err)
	}
	return policy, nil
}

// DecodeAWSIAMPermissionPolicy decodes one aws_iam_permission_policy envelope
// into the typed secretsiamv1.AWSIAMPermissionPolicy struct through the
// contracts seam, returning a self-classifying *factDecodeError on malformed
// input. The live decode site is the loader's trust-chain anchor decoder,
// which calls the SDK directly; this wrapper is the manifest's attribution
// identity for those reads (#6392).
func DecodeAWSIAMPermissionPolicy(env facts.Envelope) (secretsiamv1.AWSIAMPermissionPolicy, error) {
	policy, err := factschema.DecodeAWSIAMPermissionPolicy(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.AWSIAMPermissionPolicy{}, factdecode.NewFactDecodeError(factschema.FactKindAWSIAMPermissionPolicy, err)
	}
	return policy, nil
}

// DecodeAWSIAMPolicyAttachment decodes one aws_iam_policy_attachment envelope
// into the typed secretsiamv1.AWSIAMPolicyAttachment struct through the
// contracts seam, returning a self-classifying *factDecodeError on malformed
// input. The live decode site is the loader's trust-chain anchor decoder,
// which calls the SDK directly; this wrapper is the manifest's attribution
// identity for those reads (#6392).
func DecodeAWSIAMPolicyAttachment(env facts.Envelope) (secretsiamv1.AWSIAMPolicyAttachment, error) {
	attachment, err := factschema.DecodeAWSIAMPolicyAttachment(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.AWSIAMPolicyAttachment{}, factdecode.NewFactDecodeError(factschema.FactKindAWSIAMPolicyAttachment, err)
	}
	return attachment, nil
}

// DecodeAWSIAMPermissionBoundary decodes one aws_iam_permission_boundary
// envelope into the typed secretsiamv1.AWSIAMPermissionBoundary struct
// through the contracts seam, returning a self-classifying *factDecodeError
// on malformed input. The live decode site is the loader's trust-chain
// anchor decoder, which calls the SDK directly; this wrapper is the
// manifest's attribution identity for those reads (#6392).
func DecodeAWSIAMPermissionBoundary(env facts.Envelope) (secretsiamv1.AWSIAMPermissionBoundary, error) {
	boundary, err := factschema.DecodeAWSIAMPermissionBoundary(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.AWSIAMPermissionBoundary{}, factdecode.NewFactDecodeError(factschema.FactKindAWSIAMPermissionBoundary, err)
	}
	return boundary, nil
}

// DecodeGCPIAMPrincipal decodes one gcp_iam_principal envelope into the typed
// secretsiamv1.GCPIAMPrincipal struct through the contracts seam, returning a
// self-classifying *factDecodeError on malformed input. The live decode site
// is the loader's trust-chain anchor decoder, which calls the SDK directly;
// this wrapper is the manifest's attribution identity for those reads
// (#6392).
func DecodeGCPIAMPrincipal(env facts.Envelope) (secretsiamv1.GCPIAMPrincipal, error) {
	principal, err := factschema.DecodeGCPIAMPrincipal(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.GCPIAMPrincipal{}, factdecode.NewFactDecodeError(factschema.FactKindGCPIAMPrincipal, err)
	}
	return principal, nil
}

// DecodeGCPIAMTrustPolicy decodes one gcp_iam_trust_policy envelope into the
// typed secretsiamv1.GCPIAMTrustPolicy struct through the contracts seam,
// returning a self-classifying *factDecodeError on malformed input. The live
// decode site is the loader's trust-chain anchor decoder, which calls the SDK
// directly; this wrapper is the manifest's attribution identity for those
// reads (#6392).
func DecodeGCPIAMTrustPolicy(env facts.Envelope) (secretsiamv1.GCPIAMTrustPolicy, error) {
	policy, err := factschema.DecodeGCPIAMTrustPolicy(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.GCPIAMTrustPolicy{}, factdecode.NewFactDecodeError(factschema.FactKindGCPIAMTrustPolicy, err)
	}
	return policy, nil
}

// DecodeGCPIAMPermissionPolicy decodes one gcp_iam_permission_policy envelope
// into the typed secretsiamv1.GCPIAMPermissionPolicy struct through the
// contracts seam, returning a self-classifying *factDecodeError on malformed
// input. The live decode site is the loader's trust-chain anchor decoder,
// which calls the SDK directly; this wrapper is the manifest's attribution
// identity for those reads (#6392).
func DecodeGCPIAMPermissionPolicy(env facts.Envelope) (secretsiamv1.GCPIAMPermissionPolicy, error) {
	policy, err := factschema.DecodeGCPIAMPermissionPolicy(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.GCPIAMPermissionPolicy{}, factdecode.NewFactDecodeError(factschema.FactKindGCPIAMPermissionPolicy, err)
	}
	return policy, nil
}

// DecodeKubernetesServiceAccountTokenPosture decodes one
// k8s_service_account_token_posture envelope into the typed
// secretsiamv1.KubernetesServiceAccountTokenPosture struct through the
// contracts seam, returning a self-classifying *factDecodeError on malformed
// input. The live decode site is the loader's trust-chain anchor decoder,
// which calls the SDK directly; this wrapper is the manifest's attribution
// identity for those reads (#6392).
func DecodeKubernetesServiceAccountTokenPosture(env facts.Envelope) (secretsiamv1.KubernetesServiceAccountTokenPosture, error) {
	posture, err := factschema.DecodeKubernetesServiceAccountTokenPosture(FactschemaEnvelope(env))
	if err != nil {
		return secretsiamv1.KubernetesServiceAccountTokenPosture{}, factdecode.NewFactDecodeError(factschema.FactKindKubernetesServiceAccountTokenPosture, err)
	}
	return posture, nil
}
