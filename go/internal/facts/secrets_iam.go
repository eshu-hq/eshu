// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// AWSIAMPrincipalFactKind identifies one AWS IAM principal source fact.
	AWSIAMPrincipalFactKind = "aws_iam_principal"
	// AWSIAMTrustPolicyFactKind identifies one normalized AWS IAM role trust
	// policy statement source fact.
	AWSIAMTrustPolicyFactKind = "aws_iam_trust_policy"
	// AWSIAMPermissionPolicyFactKind identifies one normalized AWS IAM identity
	// permission policy statement source fact.
	AWSIAMPermissionPolicyFactKind = "aws_iam_permission_policy"
	// AWSIAMPolicyAttachmentFactKind identifies one managed policy attachment to
	// an IAM principal.
	AWSIAMPolicyAttachmentFactKind = "aws_iam_policy_attachment"
	// AWSIAMPermissionBoundaryFactKind identifies one permissions boundary
	// attached to an IAM principal.
	AWSIAMPermissionBoundaryFactKind = "aws_iam_permission_boundary"
	// AWSIAMInstanceProfileFactKind identifies one IAM instance profile source
	// fact.
	AWSIAMInstanceProfileFactKind = "aws_iam_instance_profile"
	// AWSIAMAccessAnalyzerFindingFactKind identifies one optional AWS IAM Access
	// Analyzer finding source fact.
	AWSIAMAccessAnalyzerFindingFactKind = "aws_iam_access_analyzer_finding"
	// GCPIAMPrincipalFactKind identifies one GCP IAM principal source fact for the
	// secrets/IAM posture family, mirroring AWSIAMPrincipalFactKind for GCP. The
	// principal is a service-account grantee observed in a Cloud Asset Inventory
	// IAM binding; its join identity is the redaction-safe member fingerprint.
	GCPIAMPrincipalFactKind = "gcp_iam_principal"
	// GCPIAMTrustPolicyFactKind identifies one GCP service-account
	// impersonation trust source fact. It records who may act as a target GCP
	// service account through IAM ServiceAccount bindings without storing raw
	// member or service-account email identity.
	GCPIAMTrustPolicyFactKind = "gcp_iam_trust_policy"
	// GCPIAMPermissionPolicyFactKind identifies one GCP IAM permission grant
	// source fact, mirroring AWSIAMPermissionPolicyFactKind: a (principal, role,
	// resource) binding granting a service-account principal a role on a resource.
	GCPIAMPermissionPolicyFactKind = "gcp_iam_permission_policy"
	// KubernetesServiceAccountFactKind identifies one Kubernetes ServiceAccount
	// source fact with redacted join identity.
	KubernetesServiceAccountFactKind = "k8s_service_account"
	// KubernetesRBACRoleFactKind identifies one Kubernetes Role or ClusterRole
	// source fact with summarized RBAC rules.
	KubernetesRBACRoleFactKind = "k8s_rbac_role"
	// KubernetesRBACBindingFactKind identifies one Kubernetes RoleBinding or
	// ClusterRoleBinding source fact with redacted subjects.
	KubernetesRBACBindingFactKind = "k8s_rbac_binding"
	// KubernetesWorkloadIdentityUseFactKind identifies one workload to
	// ServiceAccount usage source fact.
	KubernetesWorkloadIdentityUseFactKind = "k8s_workload_identity_use"
	// KubernetesGCPWorkloadIdentityBindingFactKind identifies one GKE Workload
	// Identity ServiceAccount annotation joined to an operator-declared workload
	// pool. It carries only redaction-safe GCP target and subject anchors.
	KubernetesGCPWorkloadIdentityBindingFactKind = "k8s_gcp_workload_identity_binding" // #nosec G101 -- fact-kind identifier string, not a credential
	// KubernetesServiceAccountTokenPostureFactKind identifies projected and
	// automount token posture for one ServiceAccount source identity.
	KubernetesServiceAccountTokenPostureFactKind = "k8s_service_account_token_posture" // #nosec G101 -- fact-kind identifier string, not a credential
	// EKSIRSAAnnotationFactKind identifies an EKS IRSA ServiceAccount annotation
	// source fact.
	EKSIRSAAnnotationFactKind = "eks_irsa_annotation"
	// EKSPodIdentityAssociationFactKind identifies an EKS Pod Identity
	// association source fact when association evidence is available.
	EKSPodIdentityAssociationFactKind = "eks_pod_identity_association"
	// VaultAuthMountFactKind identifies one Vault auth mount metadata source
	// fact.
	VaultAuthMountFactKind = "vault_auth_mount"
	// VaultAuthRoleFactKind identifies one Vault auth role metadata source fact.
	VaultAuthRoleFactKind = "vault_auth_role"
	// VaultACLPolicyFactKind identifies one Vault ACL policy metadata source
	// fact.
	VaultACLPolicyFactKind = "vault_acl_policy"
	// VaultIdentityEntityFactKind identifies one Vault identity entity source
	// fact.
	VaultIdentityEntityFactKind = "vault_identity_entity"
	// VaultIdentityAliasFactKind identifies one Vault identity alias source fact.
	VaultIdentityAliasFactKind = "vault_identity_alias"
	// VaultKVMetadataFactKind identifies one Vault KV v2 metadata path source
	// fact.
	VaultKVMetadataFactKind = "vault_kv_metadata"
	// VaultSecretEngineMountFactKind identifies one Vault secret engine mount
	// metadata source fact.
	VaultSecretEngineMountFactKind = "vault_secret_engine_mount"
	// SecretsIAMCoverageWarningFactKind identifies source-local coverage,
	// redaction, unsupported, partial, permission-hidden, rate-limited, or stale
	// warning evidence for the secrets/IAM posture collector family.
	SecretsIAMCoverageWarningFactKind = "secrets_iam_coverage_warning"

	// SecretsIAMSchemaVersionV1 is the first secrets/IAM posture source schema.
	SecretsIAMSchemaVersionV1 = "1.0.0"
)

var secretsIAMFactKinds = []string{
	AWSIAMPrincipalFactKind,
	AWSIAMTrustPolicyFactKind,
	AWSIAMPermissionPolicyFactKind,
	AWSIAMPolicyAttachmentFactKind,
	AWSIAMPermissionBoundaryFactKind,
	AWSIAMInstanceProfileFactKind,
	AWSIAMAccessAnalyzerFindingFactKind,
	GCPIAMPrincipalFactKind,
	GCPIAMTrustPolicyFactKind,
	GCPIAMPermissionPolicyFactKind,
	KubernetesServiceAccountFactKind,
	KubernetesRBACRoleFactKind,
	KubernetesRBACBindingFactKind,
	KubernetesWorkloadIdentityUseFactKind,
	KubernetesGCPWorkloadIdentityBindingFactKind,
	KubernetesServiceAccountTokenPostureFactKind,
	EKSIRSAAnnotationFactKind,
	EKSPodIdentityAssociationFactKind,
	VaultAuthMountFactKind,
	VaultAuthRoleFactKind,
	VaultACLPolicyFactKind,
	VaultIdentityEntityFactKind,
	VaultIdentityAliasFactKind,
	VaultKVMetadataFactKind,
	VaultSecretEngineMountFactKind,
	SecretsIAMCoverageWarningFactKind,
}

var secretsIAMSchemaVersions = map[string]string{
	AWSIAMPrincipalFactKind:                      SecretsIAMSchemaVersionV1,
	AWSIAMTrustPolicyFactKind:                    SecretsIAMSchemaVersionV1,
	AWSIAMPermissionPolicyFactKind:               SecretsIAMSchemaVersionV1,
	AWSIAMPolicyAttachmentFactKind:               SecretsIAMSchemaVersionV1,
	AWSIAMPermissionBoundaryFactKind:             SecretsIAMSchemaVersionV1,
	AWSIAMInstanceProfileFactKind:                SecretsIAMSchemaVersionV1,
	AWSIAMAccessAnalyzerFindingFactKind:          SecretsIAMSchemaVersionV1,
	GCPIAMPrincipalFactKind:                      SecretsIAMSchemaVersionV1,
	GCPIAMTrustPolicyFactKind:                    SecretsIAMSchemaVersionV1,
	GCPIAMPermissionPolicyFactKind:               SecretsIAMSchemaVersionV1,
	KubernetesServiceAccountFactKind:             SecretsIAMSchemaVersionV1,
	KubernetesRBACRoleFactKind:                   SecretsIAMSchemaVersionV1,
	KubernetesRBACBindingFactKind:                SecretsIAMSchemaVersionV1,
	KubernetesWorkloadIdentityUseFactKind:        SecretsIAMSchemaVersionV1,
	KubernetesGCPWorkloadIdentityBindingFactKind: SecretsIAMSchemaVersionV1,
	KubernetesServiceAccountTokenPostureFactKind: SecretsIAMSchemaVersionV1,
	EKSIRSAAnnotationFactKind:                    SecretsIAMSchemaVersionV1,
	EKSPodIdentityAssociationFactKind:            SecretsIAMSchemaVersionV1,
	VaultAuthMountFactKind:                       SecretsIAMSchemaVersionV1,
	VaultAuthRoleFactKind:                        SecretsIAMSchemaVersionV1,
	VaultACLPolicyFactKind:                       SecretsIAMSchemaVersionV1,
	VaultIdentityEntityFactKind:                  SecretsIAMSchemaVersionV1,
	VaultIdentityAliasFactKind:                   SecretsIAMSchemaVersionV1,
	VaultKVMetadataFactKind:                      SecretsIAMSchemaVersionV1,
	VaultSecretEngineMountFactKind:               SecretsIAMSchemaVersionV1,
	SecretsIAMCoverageWarningFactKind:            SecretsIAMSchemaVersionV1,
}

// SecretsIAMFactKinds returns the accepted secrets/IAM posture source fact
// kinds in source-contract order.
func SecretsIAMFactKinds() []string {
	return slices.Clone(secretsIAMFactKinds)
}

// SecretsIAMSchemaVersion returns the schema version for a secrets/IAM posture
// source fact kind.
func SecretsIAMSchemaVersion(factKind string) (string, bool) {
	version, ok := secretsIAMSchemaVersions[factKind]
	return version, ok
}
