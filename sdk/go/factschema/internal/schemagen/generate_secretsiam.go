// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

// AWSIAMTrustPolicySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_trust_policy" payload.
const AWSIAMTrustPolicySchemaID = schemaBaseID + "secretsiam/v1/aws_iam_trust_policy.schema.json"

// AWSIAMTrustPolicySchema returns the JSON Schema bytes for
// secretsiamv1.AWSIAMTrustPolicy.
func AWSIAMTrustPolicySchema() ([]byte, error) {
	return reflectSchema(AWSIAMTrustPolicySchemaID, "Eshu aws_iam_trust_policy Payload (schema version 1)", &secretsiamv1.AWSIAMTrustPolicy{})
}

// AWSIAMPermissionPolicySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_permission_policy" payload.
const AWSIAMPermissionPolicySchemaID = schemaBaseID + "secretsiam/v1/aws_iam_permission_policy.schema.json"

// AWSIAMPermissionPolicySchema returns the JSON Schema bytes for
// secretsiamv1.AWSIAMPermissionPolicy.
func AWSIAMPermissionPolicySchema() ([]byte, error) {
	return reflectSchema(AWSIAMPermissionPolicySchemaID, "Eshu aws_iam_permission_policy Payload (schema version 1)", &secretsiamv1.AWSIAMPermissionPolicy{})
}

// AWSIAMPolicyAttachmentSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_policy_attachment" payload.
const AWSIAMPolicyAttachmentSchemaID = schemaBaseID + "secretsiam/v1/aws_iam_policy_attachment.schema.json"

// AWSIAMPolicyAttachmentSchema returns the JSON Schema bytes for
// secretsiamv1.AWSIAMPolicyAttachment.
func AWSIAMPolicyAttachmentSchema() ([]byte, error) {
	return reflectSchema(AWSIAMPolicyAttachmentSchemaID, "Eshu aws_iam_policy_attachment Payload (schema version 1)", &secretsiamv1.AWSIAMPolicyAttachment{})
}

// AWSIAMPermissionBoundarySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_permission_boundary" payload.
const AWSIAMPermissionBoundarySchemaID = schemaBaseID + "secretsiam/v1/aws_iam_permission_boundary.schema.json"

// AWSIAMPermissionBoundarySchema returns the JSON Schema bytes for
// secretsiamv1.AWSIAMPermissionBoundary.
func AWSIAMPermissionBoundarySchema() ([]byte, error) {
	return reflectSchema(AWSIAMPermissionBoundarySchemaID, "Eshu aws_iam_permission_boundary Payload (schema version 1)", &secretsiamv1.AWSIAMPermissionBoundary{})
}

// AWSIAMInstanceProfileSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_instance_profile" payload.
const AWSIAMInstanceProfileSchemaID = schemaBaseID + "secretsiam/v1/aws_iam_instance_profile.schema.json"

// AWSIAMInstanceProfileSchema returns the JSON Schema bytes for
// secretsiamv1.AWSIAMInstanceProfile.
func AWSIAMInstanceProfileSchema() ([]byte, error) {
	return reflectSchema(AWSIAMInstanceProfileSchemaID, "Eshu aws_iam_instance_profile Payload (schema version 1)", &secretsiamv1.AWSIAMInstanceProfile{})
}

// AWSIAMAccessAnalyzerFindingSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "aws_iam_access_analyzer_finding" payload.
const AWSIAMAccessAnalyzerFindingSchemaID = schemaBaseID + "secretsiam/v1/aws_iam_access_analyzer_finding.schema.json"

// AWSIAMAccessAnalyzerFindingSchema returns the JSON Schema bytes for
// secretsiamv1.AWSIAMAccessAnalyzerFinding.
func AWSIAMAccessAnalyzerFindingSchema() ([]byte, error) {
	return reflectSchema(AWSIAMAccessAnalyzerFindingSchemaID, "Eshu aws_iam_access_analyzer_finding Payload (schema version 1)", &secretsiamv1.AWSIAMAccessAnalyzerFinding{})
}

// GCPIAMPrincipalSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_iam_principal" payload.
const GCPIAMPrincipalSchemaID = schemaBaseID + "secretsiam/v1/gcp_iam_principal.schema.json"

// GCPIAMPrincipalSchema returns the JSON Schema bytes for
// secretsiamv1.GCPIAMPrincipal.
func GCPIAMPrincipalSchema() ([]byte, error) {
	return reflectSchema(GCPIAMPrincipalSchemaID, "Eshu gcp_iam_principal Payload (schema version 1)", &secretsiamv1.GCPIAMPrincipal{})
}

// GCPIAMTrustPolicySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_iam_trust_policy" payload.
const GCPIAMTrustPolicySchemaID = schemaBaseID + "secretsiam/v1/gcp_iam_trust_policy.schema.json"

// GCPIAMTrustPolicySchema returns the JSON Schema bytes for
// secretsiamv1.GCPIAMTrustPolicy.
func GCPIAMTrustPolicySchema() ([]byte, error) {
	return reflectSchema(GCPIAMTrustPolicySchemaID, "Eshu gcp_iam_trust_policy Payload (schema version 1)", &secretsiamv1.GCPIAMTrustPolicy{})
}

// GCPIAMPermissionPolicySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_iam_permission_policy" payload.
const GCPIAMPermissionPolicySchemaID = schemaBaseID + "secretsiam/v1/gcp_iam_permission_policy.schema.json"

// GCPIAMPermissionPolicySchema returns the JSON Schema bytes for
// secretsiamv1.GCPIAMPermissionPolicy.
func GCPIAMPermissionPolicySchema() ([]byte, error) {
	return reflectSchema(GCPIAMPermissionPolicySchemaID, "Eshu gcp_iam_permission_policy Payload (schema version 1)", &secretsiamv1.GCPIAMPermissionPolicy{})
}

// KubernetesRBACRoleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "k8s_rbac_role" payload.
const KubernetesRBACRoleSchemaID = schemaBaseID + "secretsiam/v1/k8s_rbac_role.schema.json"

// KubernetesRBACRoleSchema returns the JSON Schema bytes for
// secretsiamv1.KubernetesRBACRole.
func KubernetesRBACRoleSchema() ([]byte, error) {
	return reflectSchema(KubernetesRBACRoleSchemaID, "Eshu k8s_rbac_role Payload (schema version 1)", &secretsiamv1.KubernetesRBACRole{})
}

// KubernetesRBACBindingSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "k8s_rbac_binding" payload.
const KubernetesRBACBindingSchemaID = schemaBaseID + "secretsiam/v1/k8s_rbac_binding.schema.json"

// KubernetesRBACBindingSchema returns the JSON Schema bytes for
// secretsiamv1.KubernetesRBACBinding.
func KubernetesRBACBindingSchema() ([]byte, error) {
	return reflectSchema(KubernetesRBACBindingSchemaID, "Eshu k8s_rbac_binding Payload (schema version 1)", &secretsiamv1.KubernetesRBACBinding{})
}

// KubernetesServiceAccountTokenPostureSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "k8s_service_account_token_posture" payload.
const KubernetesServiceAccountTokenPostureSchemaID = schemaBaseID + "secretsiam/v1/k8s_service_account_token_posture.schema.json"

// KubernetesServiceAccountTokenPostureSchema returns the JSON Schema bytes for
// secretsiamv1.KubernetesServiceAccountTokenPosture.
func KubernetesServiceAccountTokenPostureSchema() ([]byte, error) {
	return reflectSchema(KubernetesServiceAccountTokenPostureSchemaID, "Eshu k8s_service_account_token_posture Payload (schema version 1)", &secretsiamv1.KubernetesServiceAccountTokenPosture{})
}

// VaultAuthRoleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_auth_role" payload.
const VaultAuthRoleSchemaID = schemaBaseID + "secretsiam/v1/vault_auth_role.schema.json"

// VaultAuthRoleSchema returns the JSON Schema bytes for
// secretsiamv1.VaultAuthRole.
func VaultAuthRoleSchema() ([]byte, error) {
	return reflectSchema(VaultAuthRoleSchemaID, "Eshu vault_auth_role Payload (schema version 1)", &secretsiamv1.VaultAuthRole{})
}

// VaultACLPolicySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_acl_policy" payload.
const VaultACLPolicySchemaID = schemaBaseID + "secretsiam/v1/vault_acl_policy.schema.json"

// VaultACLPolicySchema returns the JSON Schema bytes for
// secretsiamv1.VaultACLPolicy.
func VaultACLPolicySchema() ([]byte, error) {
	return reflectSchema(VaultACLPolicySchemaID, "Eshu vault_acl_policy Payload (schema version 1)", &secretsiamv1.VaultACLPolicy{})
}

// VaultKVMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_kv_metadata" payload.
const VaultKVMetadataSchemaID = schemaBaseID + "secretsiam/v1/vault_kv_metadata.schema.json"

// VaultKVMetadataSchema returns the JSON Schema bytes for
// secretsiamv1.VaultKVMetadata.
func VaultKVMetadataSchema() ([]byte, error) {
	return reflectSchema(VaultKVMetadataSchemaID, "Eshu vault_kv_metadata Payload (schema version 1)", &secretsiamv1.VaultKVMetadata{})
}

// KubernetesServiceAccountSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "k8s_service_account" payload.
const KubernetesServiceAccountSchemaID = schemaBaseID + "secretsiam/v1/k8s_service_account.schema.json"

// KubernetesServiceAccountSchema returns the JSON Schema bytes for
// secretsiamv1.KubernetesServiceAccount.
func KubernetesServiceAccountSchema() ([]byte, error) {
	return reflectSchema(KubernetesServiceAccountSchemaID, "Eshu k8s_service_account Payload (schema version 1)", &secretsiamv1.KubernetesServiceAccount{})
}

// KubernetesWorkloadIdentityUseSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "k8s_workload_identity_use" payload.
const KubernetesWorkloadIdentityUseSchemaID = schemaBaseID + "secretsiam/v1/k8s_workload_identity_use.schema.json"

// KubernetesWorkloadIdentityUseSchema returns the JSON Schema bytes for
// secretsiamv1.KubernetesWorkloadIdentityUse.
func KubernetesWorkloadIdentityUseSchema() ([]byte, error) {
	return reflectSchema(KubernetesWorkloadIdentityUseSchemaID, "Eshu k8s_workload_identity_use Payload (schema version 1)", &secretsiamv1.KubernetesWorkloadIdentityUse{})
}

// EKSIRSAAnnotationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "eks_irsa_annotation" payload.
const EKSIRSAAnnotationSchemaID = schemaBaseID + "secretsiam/v1/eks_irsa_annotation.schema.json"

// EKSIRSAAnnotationSchema returns the JSON Schema bytes for
// secretsiamv1.EKSIRSAAnnotation.
func EKSIRSAAnnotationSchema() ([]byte, error) {
	return reflectSchema(EKSIRSAAnnotationSchemaID, "Eshu eks_irsa_annotation Payload (schema version 1)", &secretsiamv1.EKSIRSAAnnotation{})
}

// EKSPodIdentityAssociationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "eks_pod_identity_association" payload.
const EKSPodIdentityAssociationSchemaID = schemaBaseID + "secretsiam/v1/eks_pod_identity_association.schema.json"

// EKSPodIdentityAssociationSchema returns the JSON Schema bytes for
// secretsiamv1.EKSPodIdentityAssociation.
func EKSPodIdentityAssociationSchema() ([]byte, error) {
	return reflectSchema(EKSPodIdentityAssociationSchemaID, "Eshu eks_pod_identity_association Payload (schema version 1)", &secretsiamv1.EKSPodIdentityAssociation{})
}

// KubernetesGCPWorkloadIdentityBindingSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "k8s_gcp_workload_identity_binding" payload.
const KubernetesGCPWorkloadIdentityBindingSchemaID = schemaBaseID + "secretsiam/v1/k8s_gcp_workload_identity_binding.schema.json"

// KubernetesGCPWorkloadIdentityBindingSchema returns the JSON Schema bytes
// for secretsiamv1.KubernetesGCPWorkloadIdentityBinding.
func KubernetesGCPWorkloadIdentityBindingSchema() ([]byte, error) {
	return reflectSchema(KubernetesGCPWorkloadIdentityBindingSchemaID, "Eshu k8s_gcp_workload_identity_binding Payload (schema version 1)", &secretsiamv1.KubernetesGCPWorkloadIdentityBinding{})
}

// VaultAuthMountSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_auth_mount" payload.
const VaultAuthMountSchemaID = schemaBaseID + "secretsiam/v1/vault_auth_mount.schema.json"

// VaultAuthMountSchema returns the JSON Schema bytes for
// secretsiamv1.VaultAuthMount.
func VaultAuthMountSchema() ([]byte, error) {
	return reflectSchema(VaultAuthMountSchemaID, "Eshu vault_auth_mount Payload (schema version 1)", &secretsiamv1.VaultAuthMount{})
}

// VaultIdentityEntitySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_identity_entity" payload.
const VaultIdentityEntitySchemaID = schemaBaseID + "secretsiam/v1/vault_identity_entity.schema.json"

// VaultIdentityEntitySchema returns the JSON Schema bytes for
// secretsiamv1.VaultIdentityEntity.
func VaultIdentityEntitySchema() ([]byte, error) {
	return reflectSchema(VaultIdentityEntitySchemaID, "Eshu vault_identity_entity Payload (schema version 1)", &secretsiamv1.VaultIdentityEntity{})
}

// VaultIdentityAliasSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_identity_alias" payload.
const VaultIdentityAliasSchemaID = schemaBaseID + "secretsiam/v1/vault_identity_alias.schema.json"

// VaultIdentityAliasSchema returns the JSON Schema bytes for
// secretsiamv1.VaultIdentityAlias.
func VaultIdentityAliasSchema() ([]byte, error) {
	return reflectSchema(VaultIdentityAliasSchemaID, "Eshu vault_identity_alias Payload (schema version 1)", &secretsiamv1.VaultIdentityAlias{})
}

// VaultSecretEngineMountSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_secret_engine_mount" payload.
const VaultSecretEngineMountSchemaID = schemaBaseID + "secretsiam/v1/vault_secret_engine_mount.schema.json"

// VaultSecretEngineMountSchema returns the JSON Schema bytes for
// secretsiamv1.VaultSecretEngineMount.
func VaultSecretEngineMountSchema() ([]byte, error) {
	return reflectSchema(VaultSecretEngineMountSchemaID, "Eshu vault_secret_engine_mount Payload (schema version 1)", &secretsiamv1.VaultSecretEngineMount{})
}

// SecretsIAMCoverageWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "secrets_iam_coverage_warning" payload.
const SecretsIAMCoverageWarningSchemaID = schemaBaseID + "secretsiam/v1/secrets_iam_coverage_warning.schema.json"

// SecretsIAMCoverageWarningSchema returns the JSON Schema bytes for
// secretsiamv1.CoverageWarning.
func SecretsIAMCoverageWarningSchema() ([]byte, error) {
	return reflectSchema(SecretsIAMCoverageWarningSchemaID, "Eshu secrets_iam_coverage_warning Payload (schema version 1)", &secretsiamv1.CoverageWarning{})
}
