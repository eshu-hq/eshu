// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// SecretsIAMBuildSecretsIAMTrustChainReadModelsBinding declares this package's verified reducer consumption of
// secrets_iam kinds (issue #6629). Build-time metadata only.
var SecretsIAMBuildSecretsIAMTrustChainReadModelsBinding = contract.Bind(contract.Binding{
	ID: "secretsiam/secretsiam_build_secrets_i_a_m_trust_chain_read_models", FamilyID: "secrets_iam",
	ReducerDir: "go/internal/reducer/secretsiam", Symbol: "BuildSecretsIAMTrustChainReadModels",
	Kinds: []string{"aws_iam_principal", "aws_iam_trust_policy", "eks_irsa_annotation", "eks_pod_identity_association", "gcp_iam_permission_policy", "gcp_iam_principal", "gcp_iam_trust_policy", "k8s_gcp_workload_identity_binding", "k8s_service_account", "k8s_workload_identity_use", "secrets_iam_coverage_warning", "vault_acl_policy", "vault_auth_role", "vault_kv_metadata"},
})
