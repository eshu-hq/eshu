// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

// The secrets_iam family fact-kind strings are UNDERSCORE-separated, like
// the aws/gcp/azure kinds. The values here MATCH the wire strings the
// secrets_iam posture collector emits (go/internal/facts.VaultAuthRoleFactKind
// and siblings) byte-for-byte; the reducer-side drift lock
// TestFactSchemaKindsMatchWireFactKinds asserts each stays byte-equal to
// its facts.*FactKind counterpart. The legacy aws_iam_principal constant
// lives in fact_kinds_aws.go because the earlier IAM lane introduced it
// there; the rest of the secrets_iam source facts live here.
const (
	// FactKindAWSIAMTrustPolicy is the "aws_iam_trust_policy" fact kind.
	FactKindAWSIAMTrustPolicy = "aws_iam_trust_policy"
	// FactKindAWSIAMPermissionPolicy is the "aws_iam_permission_policy" fact
	// kind.
	FactKindAWSIAMPermissionPolicy = "aws_iam_permission_policy"
	// FactKindAWSIAMPolicyAttachment is the "aws_iam_policy_attachment" fact
	// kind.
	FactKindAWSIAMPolicyAttachment = "aws_iam_policy_attachment"
	// FactKindAWSIAMPermissionBoundary is the "aws_iam_permission_boundary"
	// fact kind.
	FactKindAWSIAMPermissionBoundary = "aws_iam_permission_boundary"
	// FactKindAWSIAMInstanceProfile is the "aws_iam_instance_profile" fact
	// kind.
	FactKindAWSIAMInstanceProfile = "aws_iam_instance_profile"
	// FactKindAWSIAMAccessAnalyzerFinding is the
	// "aws_iam_access_analyzer_finding" fact kind.
	FactKindAWSIAMAccessAnalyzerFinding = "aws_iam_access_analyzer_finding"
	// FactKindGCPIAMPrincipal is the "gcp_iam_principal" fact kind.
	FactKindGCPIAMPrincipal = "gcp_iam_principal"
	// FactKindGCPIAMTrustPolicy is the "gcp_iam_trust_policy" fact kind.
	FactKindGCPIAMTrustPolicy = "gcp_iam_trust_policy"
	// FactKindGCPIAMPermissionPolicy is the "gcp_iam_permission_policy" fact
	// kind.
	FactKindGCPIAMPermissionPolicy = "gcp_iam_permission_policy"
	// FactKindKubernetesRBACRole is the "k8s_rbac_role" fact kind.
	FactKindKubernetesRBACRole = "k8s_rbac_role"
	// FactKindKubernetesRBACBinding is the "k8s_rbac_binding" fact kind.
	FactKindKubernetesRBACBinding = "k8s_rbac_binding"
	// FactKindKubernetesServiceAccountTokenPosture is the
	// "k8s_service_account_token_posture" fact kind.
	FactKindKubernetesServiceAccountTokenPosture = "k8s_service_account_token_posture" // #nosec G101 -- fact-kind identifier string, not a credential

	// FactKindVaultAuthRole is the "vault_auth_role" fact kind.
	FactKindVaultAuthRole = "vault_auth_role"
	// FactKindVaultACLPolicy is the "vault_acl_policy" fact kind.
	FactKindVaultACLPolicy = "vault_acl_policy"
	// FactKindVaultKVMetadata is the "vault_kv_metadata" fact kind.
	FactKindVaultKVMetadata = "vault_kv_metadata"
	// FactKindKubernetesServiceAccount is the "k8s_service_account" fact kind.
	FactKindKubernetesServiceAccount = "k8s_service_account"
	// FactKindKubernetesWorkloadIdentityUse is the
	// "k8s_workload_identity_use" fact kind.
	FactKindKubernetesWorkloadIdentityUse = "k8s_workload_identity_use"
	// FactKindEKSIRSAAnnotation is the "eks_irsa_annotation" fact kind.
	FactKindEKSIRSAAnnotation = "eks_irsa_annotation"
	// FactKindEKSPodIdentityAssociation is the "eks_pod_identity_association"
	// fact kind.
	FactKindEKSPodIdentityAssociation = "eks_pod_identity_association"
	// FactKindKubernetesGCPWorkloadIdentityBinding is the
	// "k8s_gcp_workload_identity_binding" fact kind.
	FactKindKubernetesGCPWorkloadIdentityBinding = "k8s_gcp_workload_identity_binding" // #nosec G101 -- fact-kind identifier string, not a credential
	// FactKindVaultAuthMount is the "vault_auth_mount" fact kind.
	FactKindVaultAuthMount = "vault_auth_mount"
	// FactKindVaultIdentityEntity is the "vault_identity_entity" fact kind.
	FactKindVaultIdentityEntity = "vault_identity_entity"
	// FactKindVaultIdentityAlias is the "vault_identity_alias" fact kind.
	FactKindVaultIdentityAlias = "vault_identity_alias"
	// FactKindVaultSecretEngineMount is the "vault_secret_engine_mount" fact
	// kind.
	FactKindVaultSecretEngineMount = "vault_secret_engine_mount"
	// FactKindSecretsIAMCoverageWarning is the "secrets_iam_coverage_warning"
	// fact kind.
	FactKindSecretsIAMCoverageWarning = "secrets_iam_coverage_warning"
)
