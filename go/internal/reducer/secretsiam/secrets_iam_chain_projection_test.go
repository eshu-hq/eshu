// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	factsidentity "github.com/eshu-hq/eshu/go/internal/facts/identity"
)

// TestProjectSecretsIAMGraphRowsFullExactChainAndPath drives the exact
// workload-to-Vault chain plus an exact secret-access path from COLLECTOR
// facts through the trust-chain build, the read-model payload seam, and the
// graph-projection extractor, asserting the exact edge rows per family. It
// proves the offline chain the Ifá vacuity guards use reproduces what the
// live trust-chain -> graph-projection chain would write, without a graph
// backend.
func TestProjectSecretsIAMGraphRowsFullExactChainAndPath(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	subjectFingerprint := "sha256:web-identity-subject"
	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	policyKey := "sha256:vault-policy"
	mountKey := "sha256:vault-mount"
	kvPathFingerprint := "sha256:kv-path"
	envelopes := []facts.Envelope{
		secretsIAMReducerFact("sa", factsidentity.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", factsidentity.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
			"workload_kind":            "deployments",
		}),
		secretsIAMReducerFact("irsa", factsidentity.EKSIRSAAnnotationFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                         "kubernetes",
			"service_account_join_key":         serviceAccountKey,
			"role_arn":                         roleARN,
			"web_identity_subject_fingerprint": subjectFingerprint,
		}),
		secretsIAMReducerFact("principal", factsidentity.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":       "aws_iam",
			"principal_arn":  roleARN,
			"principal_type": "aws_iam_role",
			"account_id":     "123456789012",
			"region":         "us-east-1",
		}),
		secretsIAMReducerFact("trust", factsidentity.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":                          "aws_iam",
			"role_arn":                          roleARN,
			"effect":                            "Allow",
			"actions":                           []string{"sts:assumerolewithwebidentity"},
			"web_identity_subject_fingerprints": []string{subjectFingerprint},
			"web_identity_subject_wildcard":     false,
		}),
		secretsIAMReducerFact("vault-role", factsidentity.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"role_join_key":                   "sha256:vault-role",
			"mount_join_key":                  mountKey,
			"bound_service_account_join_keys": []string{serviceAccountKey},
			"token_policy_join_keys":          []string{policyKey},
		}),
		secretsIAMReducerFact("vault-policy", factsidentity.VaultACLPolicyFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":        "vault",
			"policy_join_key": policyKey,
			"rules": []map[string]any{{
				"path_fingerprint": kvPathFingerprint,
				"capabilities":     []string{"read"},
			}},
		}),
		secretsIAMReducerFact("vault-kv", factsidentity.VaultKVMetadataFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":            "vault",
			"mount_join_key":      mountKey,
			"kv_path_fingerprint": kvPathFingerprint,
			"path_depth":          3,
		}),
	}

	rows, quarantined, err := ProjectSecretsIAMGraphRows(envelopes, "vault-scope", "vault-gen")
	if err != nil {
		t.Fatalf("ProjectSecretsIAMGraphRows() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("ProjectSecretsIAMGraphRows() quarantined %d fact(s), want 0", len(quarantined))
	}
	if got, want := len(rows.AuthenticatesVaultRoleEdges), 1; got != want {
		t.Fatalf("AuthenticatesVaultRoleEdges = %d, want %d: %+v", got, want, rows.AuthenticatesVaultRoleEdges)
	}
	authEdge := rows.AuthenticatesVaultRoleEdges[0]
	if authEdge["service_account_uid"] != serviceAccountKey || authEdge["vault_auth_role_uid"] != "sha256:vault-role" {
		t.Fatalf("authenticates edge endpoints = %v/%v, want %v/sha256:vault-role",
			authEdge["service_account_uid"], authEdge["vault_auth_role_uid"], serviceAccountKey)
	}
	if got, want := len(rows.UsesVaultPolicyEdges), 1; got != want {
		t.Fatalf("UsesVaultPolicyEdges = %d, want %d: %+v", got, want, rows.UsesVaultPolicyEdges)
	}
	if got, want := len(rows.GrantsSecretReadEdges), 1; got != want {
		t.Fatalf("GrantsSecretReadEdges = %d, want %d: %+v", got, want, rows.GrantsSecretReadEdges)
	}
	grantsEdge := rows.GrantsSecretReadEdges[0]
	if grantsEdge["vault_policy_uid"] != policyKey {
		t.Fatalf("grants edge policy = %v, want %v", grantsEdge["vault_policy_uid"], policyKey)
	}
	if len(rows.SecretMetadataPathNodes) != 1 || grantsEdge["secret_path_uid"] != rows.SecretMetadataPathNodes[0]["uid"] {
		t.Fatalf("grants edge path uid does not match the projected secret-path node: %+v vs %+v",
			grantsEdge, rows.SecretMetadataPathNodes)
	}
}

// TestProjectSecretsIAMGraphRowsSkipsNonExact proves a partial chain yields
// no edge rows: the adapter must not promote posture evidence into graph
// truth.
func TestProjectSecretsIAMGraphRowsSkipsNonExact(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		secretsIAMReducerFact("trust", factsidentity.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":                      "aws_iam",
			"role_arn":                      "arn:aws:iam::123456789012:role/broad",
			"effect":                        "Allow",
			"actions":                       []string{"sts:assumerolewithwebidentity"},
			"web_identity_subject_wildcard": true,
		}),
	}

	rows, _, err := ProjectSecretsIAMGraphRows(envelopes, "aws-scope", "aws-gen")
	if err != nil {
		t.Fatalf("ProjectSecretsIAMGraphRows() error = %v, want nil", err)
	}
	if total := len(rows.AuthenticatesVaultRoleEdges) + len(rows.UsesVaultPolicyEdges) + len(rows.GrantsSecretReadEdges); total != 0 {
		t.Fatalf("non-exact input projected %d edge rows, want 0", total)
	}
}

// TestProjectSecretsIAMGraphRowsQuarantinesMalformed proves a malformed
// collector fact is quarantined per-fact rather than failing the whole
// projection: the valid chain still projects.
func TestProjectSecretsIAMGraphRowsQuarantinesMalformed(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	roleARN := "arn:aws:iam::123456789012:role/payments-pod-identity"
	envelopes := []facts.Envelope{
		secretsIAMReducerFact("sa", factsidentity.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", factsidentity.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
		}),
		secretsIAMReducerFact("pod-identity", factsidentity.EKSPodIdentityAssociationFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"role_arn":                 roleARN,
		}),
		secretsIAMReducerFact("principal", factsidentity.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":       "aws_iam",
			"principal_arn":  roleARN,
			"principal_type": "aws_iam_role",
			"account_id":     "123456789012",
			"region":         "us-east-1",
		}),
		secretsIAMReducerFact("trust", factsidentity.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":          "aws_iam",
			"role_arn":          roleARN,
			"effect":            "Allow",
			"actions":           []string{"sts:AssumeRole"},
			"assume_principals": []string{"pods.eks.amazonaws.com"},
		}),
		secretsIAMReducerFact("vault-role", factsidentity.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"role_join_key":                   "sha256:vault-role",
			"bound_service_account_join_keys": []string{serviceAccountKey},
		}),
		secretsIAMReducerFact("bad-principal", factsidentity.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider": "aws_iam",
		}),
	}

	rows, quarantined, err := ProjectSecretsIAMGraphRows(envelopes, "k8s-scope", "k8s-gen")
	if err != nil {
		t.Fatalf("ProjectSecretsIAMGraphRows() error = %v, want nil", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("ProjectSecretsIAMGraphRows() quarantined %d fact(s), want 1", len(quarantined))
	}
	if got, want := len(rows.AuthenticatesVaultRoleEdges), 1; got != want {
		t.Fatalf("AuthenticatesVaultRoleEdges = %d, want %d after quarantine", got, want)
	}
}
