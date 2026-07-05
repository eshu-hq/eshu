// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

// DecodeVaultAuthRole decodes env.Payload into the latest
// secretsiamv1.VaultAuthRole struct for the "vault_auth_role" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2.
// Callers (reducer handlers) receive either the decoded struct or a
// classified *DecodeError; they must never substitute a zero-value struct on
// error.
func DecodeVaultAuthRole(env Envelope) (secretsiamv1.VaultAuthRole, error) {
	return decodeLatestMajor[secretsiamv1.VaultAuthRole](FactKindVaultAuthRole, env)
}

// EncodeVaultAuthRole marshals a secretsiamv1.VaultAuthRole into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeVaultAuthRole for schema-version-1 payloads, used by collectors
// emitting this fact kind and by this module's round-trip tests.
func EncodeVaultAuthRole(role secretsiamv1.VaultAuthRole) (map[string]any, error) {
	return encodeToPayload(role)
}

// DecodeVaultACLPolicy decodes env.Payload into the latest
// secretsiamv1.VaultACLPolicy struct for the "vault_acl_policy" fact kind.
// See DecodeVaultAuthRole for the dispatch and error contract.
func DecodeVaultACLPolicy(env Envelope) (secretsiamv1.VaultACLPolicy, error) {
	return decodeLatestMajor[secretsiamv1.VaultACLPolicy](FactKindVaultACLPolicy, env)
}

// EncodeVaultACLPolicy marshals a secretsiamv1.VaultACLPolicy into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeVaultACLPolicy for schema-version-1 payloads.
func EncodeVaultACLPolicy(policy secretsiamv1.VaultACLPolicy) (map[string]any, error) {
	return encodeToPayload(policy)
}

// DecodeVaultKVMetadata decodes env.Payload into the latest
// secretsiamv1.VaultKVMetadata struct for the "vault_kv_metadata" fact kind.
// See DecodeVaultAuthRole for the dispatch and error contract.
func DecodeVaultKVMetadata(env Envelope) (secretsiamv1.VaultKVMetadata, error) {
	return decodeLatestMajor[secretsiamv1.VaultKVMetadata](FactKindVaultKVMetadata, env)
}

// EncodeVaultKVMetadata marshals a secretsiamv1.VaultKVMetadata into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeVaultKVMetadata for schema-version-1 payloads.
func EncodeVaultKVMetadata(metadata secretsiamv1.VaultKVMetadata) (map[string]any, error) {
	return encodeToPayload(metadata)
}

// DecodeKubernetesServiceAccount decodes env.Payload into the latest
// secretsiamv1.KubernetesServiceAccount struct for the "k8s_service_account"
// fact kind. See DecodeVaultAuthRole for the dispatch and error contract.
func DecodeKubernetesServiceAccount(env Envelope) (secretsiamv1.KubernetesServiceAccount, error) {
	return decodeLatestMajor[secretsiamv1.KubernetesServiceAccount](FactKindKubernetesServiceAccount, env)
}

// EncodeKubernetesServiceAccount marshals a
// secretsiamv1.KubernetesServiceAccount into the map[string]any payload shape
// an Envelope carries. It is the inverse of DecodeKubernetesServiceAccount
// for schema-version-1 payloads.
func EncodeKubernetesServiceAccount(account secretsiamv1.KubernetesServiceAccount) (map[string]any, error) {
	return encodeToPayload(account)
}

// DecodeKubernetesWorkloadIdentityUse decodes env.Payload into the latest
// secretsiamv1.KubernetesWorkloadIdentityUse struct for the
// "k8s_workload_identity_use" fact kind. See DecodeVaultAuthRole for the
// dispatch and error contract.
func DecodeKubernetesWorkloadIdentityUse(env Envelope) (secretsiamv1.KubernetesWorkloadIdentityUse, error) {
	return decodeLatestMajor[secretsiamv1.KubernetesWorkloadIdentityUse](FactKindKubernetesWorkloadIdentityUse, env)
}

// EncodeKubernetesWorkloadIdentityUse marshals a
// secretsiamv1.KubernetesWorkloadIdentityUse into the map[string]any payload
// shape an Envelope carries. It is the inverse of
// DecodeKubernetesWorkloadIdentityUse for schema-version-1 payloads.
func EncodeKubernetesWorkloadIdentityUse(use secretsiamv1.KubernetesWorkloadIdentityUse) (map[string]any, error) {
	return encodeToPayload(use)
}

// DecodeEKSIRSAAnnotation decodes env.Payload into the latest
// secretsiamv1.EKSIRSAAnnotation struct for the "eks_irsa_annotation" fact
// kind. See DecodeVaultAuthRole for the dispatch and error contract.
func DecodeEKSIRSAAnnotation(env Envelope) (secretsiamv1.EKSIRSAAnnotation, error) {
	return decodeLatestMajor[secretsiamv1.EKSIRSAAnnotation](FactKindEKSIRSAAnnotation, env)
}

// EncodeEKSIRSAAnnotation marshals a secretsiamv1.EKSIRSAAnnotation into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeEKSIRSAAnnotation for schema-version-1 payloads.
func EncodeEKSIRSAAnnotation(annotation secretsiamv1.EKSIRSAAnnotation) (map[string]any, error) {
	return encodeToPayload(annotation)
}

// DecodeEKSPodIdentityAssociation decodes env.Payload into the latest
// secretsiamv1.EKSPodIdentityAssociation struct for the
// "eks_pod_identity_association" fact kind. See DecodeVaultAuthRole for the
// dispatch and error contract.
func DecodeEKSPodIdentityAssociation(env Envelope) (secretsiamv1.EKSPodIdentityAssociation, error) {
	return decodeLatestMajor[secretsiamv1.EKSPodIdentityAssociation](FactKindEKSPodIdentityAssociation, env)
}

// EncodeEKSPodIdentityAssociation marshals a
// secretsiamv1.EKSPodIdentityAssociation into the map[string]any payload
// shape an Envelope carries. It is the inverse of
// DecodeEKSPodIdentityAssociation for schema-version-1 payloads.
func EncodeEKSPodIdentityAssociation(association secretsiamv1.EKSPodIdentityAssociation) (map[string]any, error) {
	return encodeToPayload(association)
}

// DecodeKubernetesGCPWorkloadIdentityBinding decodes env.Payload into the
// latest secretsiamv1.KubernetesGCPWorkloadIdentityBinding struct for the
// "k8s_gcp_workload_identity_binding" fact kind. See DecodeVaultAuthRole for
// the dispatch and error contract. This kind is IN SCOPE for Wave 4d (the
// K8S lane): it is the Kubernetes-side annotation joined to the GCP IAM
// trust policy the deferred gcp_iam lane still reads raw.
func DecodeKubernetesGCPWorkloadIdentityBinding(env Envelope) (secretsiamv1.KubernetesGCPWorkloadIdentityBinding, error) {
	return decodeLatestMajor[secretsiamv1.KubernetesGCPWorkloadIdentityBinding](FactKindKubernetesGCPWorkloadIdentityBinding, env)
}

// EncodeKubernetesGCPWorkloadIdentityBinding marshals a
// secretsiamv1.KubernetesGCPWorkloadIdentityBinding into the map[string]any
// payload shape an Envelope carries. It is the inverse of
// DecodeKubernetesGCPWorkloadIdentityBinding for schema-version-1 payloads.
func EncodeKubernetesGCPWorkloadIdentityBinding(binding secretsiamv1.KubernetesGCPWorkloadIdentityBinding) (map[string]any, error) {
	return encodeToPayload(binding)
}
