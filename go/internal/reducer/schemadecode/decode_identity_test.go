// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"testing"

	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
	secretsiamv1 "github.com/eshu-hq/eshu-sdk/factschema/secretsiam/v1"
	servicecatalogv1 "github.com/eshu-hq/eshu-sdk/factschema/servicecatalog/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// identityDecodeCases covers the identity seams (secrets/IAM bindings,
// service catalog, reducer-derived correlations).
func identityDecodeCases() []decodeCase {
	awsAccount := map[string]any{"account_id": "123456789012", "region": "us-east-1"}
	return []decodeCase{
		{
			name: "VaultAuthRole", kind: "vault_auth_role", target: &secretsiamv1.VaultAuthRole{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVaultAuthRole(env) },
			requiredField: "role_join_key",
			spotFields:    map[string]any{"role_join_key": "auth/kubernetes/role/app"},
		},
		{
			name: "VaultACLPolicy", kind: "vault_acl_policy", target: &secretsiamv1.VaultACLPolicy{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVaultACLPolicy(env) },
			requiredField: "policy_join_key",
			spotFields:    map[string]any{"policy_join_key": "sys/policy/app-read"},
		},
		{
			name: "VaultKVMetadata", kind: "vault_kv_metadata", target: &secretsiamv1.VaultKVMetadata{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVaultKVMetadata(env) },
			requiredField: "kv_path_fingerprint",
			spotFields:    map[string]any{"mount_join_key": "sys/mounts/secret", "kv_path_fingerprint": "fp-abc"},
		},
		{
			name: "KubernetesServiceAccount", kind: "k8s_service_account", target: &secretsiamv1.KubernetesServiceAccount{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeKubernetesServiceAccount(env) },
			requiredField: "service_account_join_key",
			spotFields:    map[string]any{"service_account_join_key": "v1:ServiceAccount:prod:app"},
		},
		{
			name: "KubernetesWorkloadIdentityUse", kind: "k8s_workload_identity_use", target: &secretsiamv1.KubernetesWorkloadIdentityUse{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeKubernetesWorkloadIdentityUse(env) },
			requiredField: "service_account_join_key",
			spotFields:    map[string]any{"service_account_join_key": "v1:ServiceAccount:prod:app"},
		},
		{
			name: "EKSIRSAAnnotation", kind: "eks_irsa_annotation", target: &secretsiamv1.EKSIRSAAnnotation{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeEKSIRSAAnnotation(env) },
			requiredField: "role_arn",
			spotFields:    map[string]any{"service_account_join_key": "v1:ServiceAccount:prod:app", "role_arn": "arn:aws:iam::123456789012:role/app"},
		},
		{
			name: "EKSPodIdentityAssociation", kind: "eks_pod_identity_association", target: &secretsiamv1.EKSPodIdentityAssociation{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeEKSPodIdentityAssociation(env) },
			requiredField: "role_arn",
			spotFields:    map[string]any{"service_account_join_key": "v1:ServiceAccount:prod:app", "role_arn": "arn:aws:iam::123456789012:role/app"},
		},
		{
			name: "KubernetesGCPWorkloadIdentityBinding", kind: "k8s_gcp_workload_identity_binding", target: &secretsiamv1.KubernetesGCPWorkloadIdentityBinding{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeKubernetesGCPWorkloadIdentityBinding(env) },
			requiredField: "service_account_join_key",
			spotFields:    map[string]any{"service_account_join_key": "v1:ServiceAccount:prod:app"},
		},
		{
			name: "AWSIAMTrustPolicy", kind: "aws_iam_trust_policy", target: &secretsiamv1.AWSIAMTrustPolicy{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSIAMTrustPolicy(env) },
			requiredField: "role_arn",
			spotFields:    mergeMaps(awsAccount, map[string]any{"role_arn": "arn:aws:iam::123456789012:role/app", "effect": "Allow"}),
		},
		{
			name: "AWSIAMPermissionPolicy", kind: "aws_iam_permission_policy", target: &secretsiamv1.AWSIAMPermissionPolicy{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSIAMPermissionPolicy(env) },
			requiredField: "policy_source",
			spotFields:    mergeMaps(awsAccount, map[string]any{"principal_arn": "arn:aws:iam::123456789012:role/app", "effect": "Allow"}),
		},
		{
			name: "AWSIAMPolicyAttachment", kind: "aws_iam_policy_attachment", target: &secretsiamv1.AWSIAMPolicyAttachment{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSIAMPolicyAttachment(env) },
			requiredField: "policy_arn",
			spotFields:    mergeMaps(awsAccount, map[string]any{"principal_arn": "arn:aws:iam::123456789012:role/app", "policy_arn": "arn:aws:iam::aws:policy/ReadOnlyAccess"}),
		},
		{
			name: "AWSIAMPermissionBoundary", kind: "aws_iam_permission_boundary", target: &secretsiamv1.AWSIAMPermissionBoundary{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSIAMPermissionBoundary(env) },
			requiredField: "boundary_policy_arn",
			spotFields:    mergeMaps(awsAccount, map[string]any{"principal_arn": "arn:aws:iam::123456789012:role/app", "boundary_policy_arn": "arn:aws:iam::123456789012:policy/boundary"}),
		},
		{
			name: "GCPIAMPrincipal", kind: "gcp_iam_principal", target: &secretsiamv1.GCPIAMPrincipal{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeGCPIAMPrincipal(env) },
			requiredField: "principal_fingerprint",
			spotFields:    map[string]any{"principal_fingerprint": "fp-user", "principal_type": "user"},
		},
		{
			name: "GCPIAMTrustPolicy", kind: "gcp_iam_trust_policy", target: &secretsiamv1.GCPIAMTrustPolicy{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeGCPIAMTrustPolicy(env) },
			requiredField: "role",
			spotFields:    map[string]any{"target_principal_fingerprint": "fp-sa", "role": "roles/iam.workloadIdentityUser", "impersonation_mode": "direct"},
		},
		{
			name: "GCPIAMPermissionPolicy", kind: "gcp_iam_permission_policy", target: &secretsiamv1.GCPIAMPermissionPolicy{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeGCPIAMPermissionPolicy(env) },
			requiredField: "resource_full_resource_name",
			spotFields:    map[string]any{"principal_fingerprint": "fp-user", "role": "roles/viewer", "resource_full_resource_name": "//cloudresourcemanager.googleapis.com/projects/p"},
		},
		{
			name: "KubernetesServiceAccountTokenPosture", kind: "k8s_service_account_token_posture", target: &secretsiamv1.KubernetesServiceAccountTokenPosture{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeKubernetesServiceAccountTokenPosture(env) },
			requiredField: "service_account_join_key",
			spotFields:    map[string]any{"cluster_id": "prod-eu", "service_account_join_key": "v1:ServiceAccount:prod:app"},
		},
		{
			name: "ServiceCatalogEntity", kind: "service_catalog.entity", target: &servicecatalogv1.Entity{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeServiceCatalogEntity(env) },
			requiredField: "entity_ref",
			spotFields:    map[string]any{"entity_ref": "component:default/api"},
		},
		{
			name: "ServiceCatalogOwnership", kind: "service_catalog.ownership", target: &servicecatalogv1.Ownership{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeServiceCatalogOwnership(env) },
			requiredField: "entity_ref",
			spotFields:    map[string]any{"entity_ref": "component:default/api"},
		},
		{
			name: "ServiceCatalogRepositoryLink", kind: "service_catalog.repository_link", target: &servicecatalogv1.RepositoryLink{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeServiceCatalogRepositoryLink(env) },
			requiredField: "entity_ref",
			spotFields:    map[string]any{"entity_ref": "component:default/api"},
		},
		{
			name: "ReducerPackageOwnershipCorrelation", kind: "reducer_package_ownership_correlation", target: &reducerderivedv1.PackageOwnershipCorrelation{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeReducerPackageOwnershipCorrelation(env) },
			requiredField: "package_id",
			spotFields:    map[string]any{"package_id": "npm:lodash"},
		},
		{
			name: "ReducerPackageConsumptionCorrelation", kind: "reducer_package_consumption_correlation", target: &reducerderivedv1.PackageConsumptionCorrelation{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeReducerPackageConsumptionCorrelation(env) },
			requiredField: "package_id",
			spotFields:    map[string]any{"package_id": "npm:lodash"},
		},
		{
			name: "ReducerPackagePublicationCorrelation", kind: "reducer_package_publication_correlation", target: &reducerderivedv1.PackagePublicationCorrelation{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeReducerPackagePublicationCorrelation(env) },
			requiredField: "package_id",
			spotFields:    map[string]any{"package_id": "npm:lodash"},
		},
	}
}

func mergeMaps(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func TestIdentityDecodeSeams(t *testing.T) {
	t.Parallel()
	runDecodeCases(t, identityDecodeCases())
}
