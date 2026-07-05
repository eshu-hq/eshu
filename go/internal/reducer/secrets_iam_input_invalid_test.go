// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildSecretsIAMTrustChainReadModelsQuarantinesVaultAuthRoleMissingRoleJoinKey
// is a flagship regression test for Wave 4d of Contract System v1 (issue
// #4566/#4582): the secrets_iam family's VAULT-lane typed-decode migration. It
// proves the accuracy guarantee the migration exists to protect AND the
// per-fact isolation contract every prior wave established: a vault_auth_role
// fact missing its required role_join_key is QUARANTINED as a visible
// input_invalid dead-letter -- never silently indexed under an empty-string
// join key -- while a VALID sibling workload-to-vault chain in the same batch
// still resolves its identity trust chain (per-fact isolation, not a
// whole-intent failure).
//
// Before the migration this behavior was impossible: buildSecretsIAMIndex
// read role_join_key with a raw payloadString/payloadStrings lookup, which
// returns "" for an absent key, and addByKey's own blank-key guard silently
// dropped the malformed vault_auth_role from index.vaultRoles with no
// operator-visible signal -- not even a gap.
//
// After the migration buildSecretsIAMIndex decodes each vault_auth_role fact
// through factschema.DecodeVaultAuthRole; the malformed fact yields a
// classified *factDecodeError that partitionDecodeFailures routes to a
// per-fact quarantine, and the valid sibling's exact chain still resolves.
func TestBuildSecretsIAMTrustChainReadModelsQuarantinesVaultAuthRoleMissingRoleJoinKey(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	otherServiceAccountKey := "sha256:sa-quarantined-vault-role"
	subjectFingerprint := "sha256:web-identity-subject"
	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	policyKey := "sha256:vault-policy"
	mountKey := "sha256:vault-mount"
	kvPathFingerprint := "sha256:kv-path"

	// A vault_auth_role fact whose required role_join_key is ABSENT (not
	// merely empty): the exact malformed input the AC names. Everything else
	// is present so the ONLY reason to quarantine the fact is the missing
	// required field.
	malformedVaultRole := facts.Envelope{
		FactID:        "malformed-vault-auth-role",
		FactKind:      facts.VaultAuthRoleFactKind,
		ScopeID:       "vault-scope",
		GenerationID:  "vault-gen",
		SchemaVersion: facts.SecretsIAMSchemaVersionV1,
		Payload: map[string]any{
			// "role_join_key" intentionally absent.
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"mount_join_key":                  mountKey,
			"bound_service_account_join_keys": []string{otherServiceAccountKey},
			"token_policy_join_keys":          []string{policyKey},
		},
	}

	// A fully valid, independent workload-to-vault chain that must still
	// resolve despite the malformed vault_auth_role sharing the batch. This is
	// the isolation half of the contract: valid facts are unaffected by a
	// poisoned sibling.
	validEnvelopes := []facts.Envelope{
		secretsIAMReducerFact("sa", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
			"workload_kind":            "deployments",
		}),
		secretsIAMReducerFact("irsa", facts.EKSIRSAAnnotationFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                         "kubernetes",
			"service_account_join_key":         serviceAccountKey,
			"role_arn":                         roleARN,
			"web_identity_subject_fingerprint": subjectFingerprint,
		}),
		secretsIAMReducerFact("principal", facts.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":       "aws_iam",
			"principal_arn":  roleARN,
			"principal_type": "aws_iam_role",
			"account_id":     "123456789012",
			"region":         "us-east-1",
		}),
		secretsIAMReducerFact("trust", facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":                          "aws_iam",
			"role_arn":                          roleARN,
			"effect":                            "Allow",
			"actions":                           []string{"sts:assumerolewithwebidentity"},
			"web_identity_subject_fingerprints": []string{subjectFingerprint},
			"web_identity_subject_wildcard":     false,
		}),
		secretsIAMReducerFact("vault-role-valid", facts.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"role_join_key":                   "sha256:vault-role-valid",
			"mount_join_key":                  mountKey,
			"bound_service_account_join_keys": []string{serviceAccountKey},
			"token_policy_join_keys":          []string{policyKey},
		}),
		secretsIAMReducerFact("vault-policy", facts.VaultACLPolicyFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":        "vault",
			"policy_join_key": policyKey,
			"rules": []map[string]any{{
				"path_fingerprint": kvPathFingerprint,
				"capabilities":     []string{"read"},
			}},
		}),
		secretsIAMReducerFact("vault-kv", facts.VaultKVMetadataFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":            "vault",
			"mount_join_key":      mountKey,
			"kv_path_fingerprint": kvPathFingerprint,
			"path_depth":          3,
		}),
	}

	envelopes := append([]facts.Envelope{malformedVaultRole}, validEnvelopes...)

	models, quarantined, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	if got, want := len(quarantined), 1; got != want {
		t.Fatalf("quarantined len = %d, want %d: %#v", got, want, quarantined)
	}
	q := quarantined[0]
	if got, want := q.factID, "malformed-vault-auth-role"; got != want {
		t.Fatalf("quarantined[0].factID = %q, want %q", got, want)
	}
	if got, want := q.field, "role_join_key"; got != want {
		t.Fatalf("quarantined[0].field = %q, want %q", got, want)
	}
	if got, want := q.classification, "input_invalid"; got != want {
		t.Fatalf("quarantined[0].classification = %q, want %q", got, want)
	}

	// The valid sibling chain must still resolve to exact despite the
	// quarantined vault_auth_role sharing the batch.
	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d: %#v", got, want, models.IdentityTrustChains)
	}
	chain := models.IdentityTrustChains[0]
	if got, want := chain.State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("chain.State = %q, want %q", got, want)
	}
	if got, want := chain.VaultRoleJoinKey, "sha256:vault-role-valid"; got != want {
		t.Fatalf("chain.VaultRoleJoinKey = %q, want %q", got, want)
	}
	if got, want := len(models.SecretAccessPaths), 1; got != want {
		t.Fatalf("SecretAccessPaths len = %d, want %d: %#v", got, want, models.SecretAccessPaths)
	}
}

// TestBuildSecretsIAMTrustChainReadModelsQuarantinesK8sServiceAccountMissingJoinKey
// mirrors the vault regression above for the K8S lane: a k8s_service_account
// fact missing its required service_account_join_key is QUARANTINED rather
// than silently dropped by addByKey's blank-key guard, while a valid sibling
// service account's exact chain still resolves.
func TestBuildSecretsIAMTrustChainReadModelsQuarantinesK8sServiceAccountMissingJoinKey(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	subjectFingerprint := "sha256:web-identity-subject"
	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	policyKey := "sha256:vault-policy"
	mountKey := "sha256:vault-mount"
	kvPathFingerprint := "sha256:kv-path"

	// A k8s_service_account fact whose required service_account_join_key is
	// ABSENT.
	malformedServiceAccount := facts.Envelope{
		FactID:        "malformed-k8s-service-account",
		FactKind:      facts.KubernetesServiceAccountFactKind,
		ScopeID:       "k8s-scope",
		GenerationID:  "k8s-gen",
		SchemaVersion: facts.SecretsIAMSchemaVersionV1,
		Payload: map[string]any{
			// "service_account_join_key" intentionally absent.
			"provider": "kubernetes",
		},
	}

	validEnvelopes := []facts.Envelope{
		secretsIAMReducerFact("sa-valid", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
			"workload_kind":            "deployments",
		}),
		secretsIAMReducerFact("irsa", facts.EKSIRSAAnnotationFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                         "kubernetes",
			"service_account_join_key":         serviceAccountKey,
			"role_arn":                         roleARN,
			"web_identity_subject_fingerprint": subjectFingerprint,
		}),
		secretsIAMReducerFact("principal", facts.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":       "aws_iam",
			"principal_arn":  roleARN,
			"principal_type": "aws_iam_role",
			"account_id":     "123456789012",
			"region":         "us-east-1",
		}),
		secretsIAMReducerFact("trust", facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":                          "aws_iam",
			"role_arn":                          roleARN,
			"effect":                            "Allow",
			"actions":                           []string{"sts:assumerolewithwebidentity"},
			"web_identity_subject_fingerprints": []string{subjectFingerprint},
			"web_identity_subject_wildcard":     false,
		}),
		secretsIAMReducerFact("vault-role", facts.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"role_join_key":                   "sha256:vault-role",
			"mount_join_key":                  mountKey,
			"bound_service_account_join_keys": []string{serviceAccountKey},
			"token_policy_join_keys":          []string{policyKey},
		}),
		secretsIAMReducerFact("vault-policy", facts.VaultACLPolicyFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":        "vault",
			"policy_join_key": policyKey,
			"rules": []map[string]any{{
				"path_fingerprint": kvPathFingerprint,
				"capabilities":     []string{"read"},
			}},
		}),
		secretsIAMReducerFact("vault-kv", facts.VaultKVMetadataFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":            "vault",
			"mount_join_key":      mountKey,
			"kv_path_fingerprint": kvPathFingerprint,
			"path_depth":          3,
		}),
	}

	envelopes := append([]facts.Envelope{malformedServiceAccount}, validEnvelopes...)

	models, quarantined, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	if got, want := len(quarantined), 1; got != want {
		t.Fatalf("quarantined len = %d, want %d: %#v", got, want, quarantined)
	}
	q := quarantined[0]
	if got, want := q.factID, "malformed-k8s-service-account"; got != want {
		t.Fatalf("quarantined[0].factID = %q, want %q", got, want)
	}
	if got, want := q.field, "service_account_join_key"; got != want {
		t.Fatalf("quarantined[0].field = %q, want %q", got, want)
	}
	if got, want := q.classification, "input_invalid"; got != want {
		t.Fatalf("quarantined[0].classification = %q, want %q", got, want)
	}

	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d: %#v", got, want, models.IdentityTrustChains)
	}
	chain := models.IdentityTrustChains[0]
	if got, want := chain.State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("chain.State = %q, want %q", got, want)
	}
	if got, want := chain.ServiceAccountJoinKey, serviceAccountKey; got != want {
		t.Fatalf("chain.ServiceAccountJoinKey = %q, want %q", got, want)
	}
}

// TestBuildSecretsIAMTrustChainReadModelsQuarantinesGCPBindingMissingEmailDigest
// mirrors the vault/k8s regressions above for the K8S-lane
// k8s_gcp_workload_identity_binding kind: a binding fact missing its required
// gcp_service_account_email_digest is QUARANTINED rather than silently
// dropped by addByKey's blank-key guard, while a valid sibling service
// account's GCP exact chain still resolves.
func TestBuildSecretsIAMTrustChainReadModelsQuarantinesGCPBindingMissingEmailDigest(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	otherServiceAccountKey := "sha256:sa-quarantined-gcp-binding"
	targetFingerprint := "sha256:gcp-service-account"
	targetEmailDigest := "sha256:gcp-service-account-email"
	subjectFingerprint := "sha256:gke-subject"

	// A k8s_gcp_workload_identity_binding fact whose required
	// gcp_service_account_email_digest is ABSENT.
	malformedBinding := facts.Envelope{
		FactID:        "malformed-gcp-binding",
		FactKind:      facts.KubernetesGCPWorkloadIdentityBindingFactKind,
		ScopeID:       "k8s-scope",
		GenerationID:  "k8s-gen",
		SchemaVersion: facts.SecretsIAMSchemaVersionV1,
		Payload: map[string]any{
			// "gcp_service_account_email_digest" intentionally absent.
			"provider":                 "kubernetes",
			"service_account_join_key": otherServiceAccountKey,
			"gcp_workload_identity_subject_fingerprint": subjectFingerprint,
		},
	}

	validEnvelopes := []facts.Envelope{
		secretsIAMReducerFact("sa", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
			"workload_kind":            "deployments",
		}),
		secretsIAMReducerFact("k8s-gcp-binding-valid", facts.KubernetesGCPWorkloadIdentityBindingFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                                  "kubernetes",
			"service_account_join_key":                  serviceAccountKey,
			"gcp_service_account_email_digest":          targetEmailDigest,
			"gcp_workload_identity_subject_fingerprint": subjectFingerprint,
		}),
		gcpPrincipalFact(targetFingerprint),
		secretsIAMReducerFact("gcp-trust", facts.GCPIAMTrustPolicyFactKind, "gcp-scope", "gcp-gen", map[string]any{
			"provider":                                  "gcp_iam",
			"target_principal_fingerprint":              targetFingerprint,
			"target_service_account_email_digest":       targetEmailDigest,
			"target_service_account_cloud_resource_uid": "gcp-cloud-resource-sa",
			"gcp_workload_identity_subject_fingerprint": subjectFingerprint,
			"impersonation_mode":                        "workload_identity",
			"role":                                      "roles/iam.workloadIdentityUser",
		}),
	}

	envelopes := append([]facts.Envelope{malformedBinding}, validEnvelopes...)

	models, quarantined, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	if got, want := len(quarantined), 1; got != want {
		t.Fatalf("quarantined len = %d, want %d: %#v", got, want, quarantined)
	}
	q := quarantined[0]
	if got, want := q.factID, "malformed-gcp-binding"; got != want {
		t.Fatalf("quarantined[0].factID = %q, want %q", got, want)
	}
	if got, want := q.field, "gcp_service_account_email_digest"; got != want {
		t.Fatalf("quarantined[0].field = %q, want %q", got, want)
	}
	if got, want := q.classification, "input_invalid"; got != want {
		t.Fatalf("quarantined[0].classification = %q, want %q", got, want)
	}

	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d: %#v", got, want, models.IdentityTrustChains)
	}
	chain := models.IdentityTrustChains[0]
	if got, want := chain.State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("chain.State = %q, want %q", got, want)
	}
	if got, want := chain.GCPServiceAccountFingerprint, targetFingerprint; got != want {
		t.Fatalf("chain.GCPServiceAccountFingerprint = %q, want %q", got, want)
	}
}
