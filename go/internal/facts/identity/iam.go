// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

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

// SecretsIAMFamilyDeclaration is the typed fact-family contract declaration for the
// secrets_iam family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var SecretsIAMFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "secrets_iam",
	FactDir:                "go/internal/facts/identity",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "secrets_iam_trust_chain",
	ProjectionHook:         "secrets_iam_graph_projection",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/secrets-iam/posture-summary",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "aws_iam_access_analyzer_finding", PayloadSchema: "sdk/go/factschema/schema/aws_iam_access_analyzer_finding.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "aws_iam_instance_profile", PayloadSchema: "sdk/go/factschema/schema/aws_iam_instance_profile.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "aws_iam_permission_boundary", PayloadSchema: "sdk/go/factschema/schema/aws_iam_permission_boundary.v1.schema.json"},
		{Kind: "aws_iam_permission_policy", PayloadSchema: "sdk/go/factschema/schema/aws_iam_permission_policy.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/storage/postgres"},
		{Kind: "aws_iam_policy_attachment", PayloadSchema: "sdk/go/factschema/schema/aws_iam_policy_attachment.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/storage/postgres"},
		{Kind: "aws_iam_principal", PayloadSchema: "sdk/go/factschema/schema/aws_iam_principal.v1.schema.json"},
		{Kind: "aws_iam_trust_policy", PayloadSchema: "sdk/go/factschema/schema/aws_iam_trust_policy.v1.schema.json"},
		{Kind: "eks_irsa_annotation", PayloadSchema: "sdk/go/factschema/schema/eks_irsa_annotation.v1.schema.json"},
		{Kind: "eks_pod_identity_association", PayloadSchema: "sdk/go/factschema/schema/eks_pod_identity_association.v1.schema.json"},
		{Kind: "gcp_iam_permission_policy", PayloadSchema: "sdk/go/factschema/schema/gcp_iam_permission_policy.v1.schema.json"},
		{Kind: "gcp_iam_principal", PayloadSchema: "sdk/go/factschema/schema/gcp_iam_principal.v1.schema.json"},
		{Kind: "gcp_iam_trust_policy", PayloadSchema: "sdk/go/factschema/schema/gcp_iam_trust_policy.v1.schema.json"},
		{Kind: "k8s_gcp_workload_identity_binding", PayloadSchema: "sdk/go/factschema/schema/k8s_gcp_workload_identity_binding.v1.schema.json"},
		{Kind: "k8s_rbac_binding", PayloadSchema: "sdk/go/factschema/schema/k8s_rbac_binding.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "k8s_rbac_role", PayloadSchema: "sdk/go/factschema/schema/k8s_rbac_role.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "k8s_service_account", PayloadSchema: "sdk/go/factschema/schema/k8s_service_account.v1.schema.json"},
		{Kind: "k8s_service_account_token_posture", PayloadSchema: "sdk/go/factschema/schema/k8s_service_account_token_posture.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/storage/postgres"},
		{Kind: "k8s_workload_identity_use", PayloadSchema: "sdk/go/factschema/schema/k8s_workload_identity_use.v1.schema.json"},
		{Kind: "secrets_iam_coverage_warning", PayloadSchema: "sdk/go/factschema/schema/secrets_iam_coverage_warning.v1.schema.json"},
		{Kind: "vault_acl_policy", PayloadSchema: "sdk/go/factschema/schema/vault_acl_policy.v1.schema.json"},
		{Kind: "vault_auth_mount", PayloadSchema: "sdk/go/factschema/schema/vault_auth_mount.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "vault_auth_role", PayloadSchema: "sdk/go/factschema/schema/vault_auth_role.v1.schema.json"},
		{Kind: "vault_identity_alias", PayloadSchema: "sdk/go/factschema/schema/vault_identity_alias.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "vault_identity_entity", PayloadSchema: "sdk/go/factschema/schema/vault_identity_entity.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "vault_kv_metadata", PayloadSchema: "sdk/go/factschema/schema/vault_kv_metadata.v1.schema.json"},
		{Kind: "vault_secret_engine_mount", PayloadSchema: "sdk/go/factschema/schema/vault_secret_engine_mount.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
	},
	Producers: []contract.Producer{
		{ID: "secrets-iam-posture", Repository: "eshu-hq/eshu", Path: "go/internal/collector/access/posture", AuthorityMode: "core_issued_grant", Kinds: []string{"aws_iam_access_analyzer_finding", "aws_iam_instance_profile", "aws_iam_permission_boundary", "aws_iam_permission_policy", "aws_iam_policy_attachment", "aws_iam_principal", "aws_iam_trust_policy", "eks_irsa_annotation", "eks_pod_identity_association", "gcp_iam_permission_policy", "gcp_iam_principal", "gcp_iam_trust_policy", "k8s_gcp_workload_identity_binding", "k8s_rbac_binding", "k8s_rbac_role", "k8s_service_account", "k8s_service_account_token_posture", "k8s_workload_identity_use", "secrets_iam_coverage_warning", "vault_acl_policy", "vault_auth_mount", "vault_auth_role", "vault_identity_alias", "vault_identity_entity", "vault_kv_metadata", "vault_secret_engine_mount"}},
	},
})
