// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildSecretsIAMTrustChainReadModelsSkipsWhitespaceOnlyServiceAccountJoinKey
// proves the trim-and-skip contract every add*ByKey helper in
// secrets_iam_trust_chain_helpers.go carries: the factschema decode seam
// treats a present-but-EMPTY (or whitespace-only) required string as a VALID
// decode -- present-but-empty is not the same as absent, and only absent
// dead-letters as input_invalid (sdk/go/factschema AGENTS.md). That means a
// k8s_service_account fact with service_account_join_key: "   " decodes
// cleanly with NO quarantine. If addServiceAccountByKey/addWorkloadByKey/
// addIRSAByKey/addVaultRoleByKey indexed those facts under their raw
// (untrimmed) key instead of skipping a blank one, two INDEPENDENT
// blank-keyed facts (one whitespace-only, one truly empty -- both trim to
// "") would collide under the shared "" index entry and, given full
// identity-provider evidence under that same blank key, resolve into a
// spurious EXACT workload-to-vault-secret trust chain that never should have
// existed: the join key that produced it was never a real identity, just an
// artifact of two unrelated malformed-but-technically-valid facts sharing an
// empty string.
//
// This test builds a COMPLETE blank-key evidence set (service account +
// workload + IRSA + principal + trust + vault role + vault policy + vault
// KV, every join field blank or matching the blank key) so that removing the
// trim-and-skip guard would let a full spurious chain form, not merely an
// unresolved gap -- proving the guard, not some unrelated missing-evidence
// gap, is what keeps this from happening. It asserts: (1) zero quarantines
// (present-but-blank is a valid decode), (2) exactly ONE identity trust
// chain -- the valid sibling's -- because the blank-keyed evidence must
// never be indexed at all, and (3) the valid sibling's own exact chain is
// completely unaffected.
func TestBuildSecretsIAMTrustChainReadModelsSkipsWhitespaceOnlyServiceAccountJoinKey(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	subjectFingerprint := "sha256:web-identity-subject"
	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	policyKey := "sha256:vault-policy"
	mountKey := "sha256:vault-mount"
	kvPathFingerprint := "sha256:kv-path"

	blankSubjectFingerprint := "sha256:blank-web-identity-subject"
	blankRoleARN := "arn:aws:iam::123456789012:role/blank-key-role"
	blankPolicyKey := "sha256:blank-vault-policy"
	blankMountKey := "sha256:blank-vault-mount"
	blankKVPathFingerprint := "sha256:blank-kv-path"

	// A COMPLETE blank-key evidence set: every fact that would need to line
	// up to resolve an exact workload-to-vault-secret chain, but every
	// join-key field is an empty string (present, not absent, so none of
	// these dead-letter as input_invalid -- present-but-empty is a VALID
	// decode). Every fact here shares the identical "" key on purpose: that
	// is what actually collides into one shared index entry if the
	// trim-and-skip guard is missing from an add*ByKey helper. A
	// whitespace-only key ("   ", "\t") decodes just as validly and also
	// trims to "" (see the sibling helpers' doc comments), so it is the same
	// bug class; this test pins the exact-empty-string case because it is
	// the one that collides even WITHOUT trimming, proving the guard itself
	// -- not just the trim -- is load-bearing.
	blankKeyEnvelopes := []facts.Envelope{
		{
			FactID:        "blank-key-service-account",
			FactKind:      facts.KubernetesServiceAccountFactKind,
			ScopeID:       "k8s-scope",
			GenerationID:  "k8s-gen",
			SchemaVersion: facts.SecretsIAMSchemaVersionV1,
			Payload: map[string]any{
				"provider":                 "kubernetes",
				"service_account_join_key": "",
			},
		},
		{
			FactID:        "blank-key-workload",
			FactKind:      facts.KubernetesWorkloadIdentityUseFactKind,
			ScopeID:       "k8s-scope",
			GenerationID:  "k8s-gen",
			SchemaVersion: facts.SecretsIAMSchemaVersionV1,
			Payload: map[string]any{
				"provider":                 "kubernetes",
				"service_account_join_key": "",
				"workload_object_id":       "blank-workload-stable-id",
				"workload_kind":            "deployments",
			},
		},
		{
			FactID:        "blank-key-irsa",
			FactKind:      facts.EKSIRSAAnnotationFactKind,
			ScopeID:       "k8s-scope",
			GenerationID:  "k8s-gen",
			SchemaVersion: facts.SecretsIAMSchemaVersionV1,
			Payload: map[string]any{
				"provider":                         "kubernetes",
				"service_account_join_key":         "",
				"role_arn":                         blankRoleARN,
				"web_identity_subject_fingerprint": blankSubjectFingerprint,
			},
		},
		secretsIAMReducerFact("blank-key-principal", facts.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":       "aws_iam",
			"principal_arn":  blankRoleARN,
			"principal_type": "aws_iam_role",
			"account_id":     "123456789012",
			"region":         "us-east-1",
		}),
		secretsIAMReducerFact("blank-key-trust", facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":                          "aws_iam",
			"role_arn":                          blankRoleARN,
			"effect":                            "Allow",
			"actions":                           []string{"sts:assumerolewithwebidentity"},
			"web_identity_subject_fingerprints": []string{blankSubjectFingerprint},
			"web_identity_subject_wildcard":     false,
		}),
		{
			FactID:        "blank-key-vault-role",
			FactKind:      facts.VaultAuthRoleFactKind,
			ScopeID:       "vault-scope",
			GenerationID:  "vault-gen",
			SchemaVersion: facts.SecretsIAMSchemaVersionV1,
			Payload: map[string]any{
				"provider":                        "vault",
				"auth_method":                     "kubernetes",
				"role_join_key":                   "sha256:blank-key-vault-role",
				"mount_join_key":                  blankMountKey,
				"bound_service_account_join_keys": []string{""},
				"token_policy_join_keys":          []string{blankPolicyKey},
			},
		},
		secretsIAMReducerFact("blank-key-vault-policy", facts.VaultACLPolicyFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":        "vault",
			"policy_join_key": blankPolicyKey,
			"rules": []map[string]any{{
				"path_fingerprint": blankKVPathFingerprint,
				"capabilities":     []string{"read"},
			}},
		}),
		secretsIAMReducerFact("blank-key-vault-kv", facts.VaultKVMetadataFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":            "vault",
			"mount_join_key":      blankMountKey,
			"kv_path_fingerprint": blankKVPathFingerprint,
			"path_depth":          3,
		}),
	}

	validEnvelopes := []facts.Envelope{
		secretsIAMReducerFact("sa-valid", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload-valid", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
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

	envelopes := append(append([]facts.Envelope{}, blankKeyEnvelopes...), validEnvelopes...)

	models, quarantined, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	// Present-but-whitespace is a VALID decode, not input_invalid: none of the
	// blank-keyed facts should be quarantined.
	if got, want := len(quarantined), 0; got != want {
		t.Fatalf("quarantined len = %d, want %d (whitespace-only join key is a valid decode, not input_invalid): %#v", got, want, quarantined)
	}

	// Exactly ONE identity trust chain: the valid sibling. If the blank-key
	// guard were missing from any add*ByKey helper, the complete blank-key
	// evidence set above would collide under the shared "" index key and
	// resolve into a second, spurious EXACT chain here.
	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d (a blank/whitespace join key must never be indexed or form a chain): %#v", got, want, models.IdentityTrustChains)
	}
	chain := models.IdentityTrustChains[0]
	if got, want := chain.State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("chain.State = %q, want %q", got, want)
	}
	if got, want := chain.ServiceAccountJoinKey, serviceAccountKey; got != want {
		t.Fatalf("chain.ServiceAccountJoinKey = %q, want %q", got, want)
	}
}
