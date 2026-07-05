// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// buildSecretsIAMHandlerBenchmarkCorpus returns a realistic per-scope-
// generation evidence packet exercising every VAULT + K8S lane kind this wave
// converted. Each of the count service accounts gets one full exact
// workload-to-vault-secret chain: k8s_service_account,
// k8s_workload_identity_use, eks_irsa_annotation, aws_iam_principal,
// aws_iam_trust_policy, vault_auth_role (with a multi-rule vault_acl_policy --
// the []VaultACLPolicyRule nested-array shape the caveat calls out as the
// decode_map.go marshal-fallback risk that regressed Wave 4b/4c until their
// own float64/map[string]string fast paths landed), and vault_kv_metadata.
//
// The repo's existing gated perfcontract benchmark for this handler,
// BenchmarkSecretsIAMGCPGrantObservations (handler_budget_secrets_iam_gcp_
// grant_observations, ceiling 8,300,000 ns/op,
// testdata/benchmarks/reducer-handler-budgets.txt), builds its index purely
// from gcp_iam_principal/gcp_iam_permission_policy facts -- the deferred GCP
// IAM lane this wave leaves untouched -- so buildSecretsIAMIndex's VAULT/K8S
// switch arms this wave converted are never exercised by that specific
// benchmark; a before/after run on this branch showed no measurable
// difference (see the reducer AGENTS.md evidence note). This benchmark is the
// supplementary proof that DOES exercise the converted decode paths.
func buildSecretsIAMHandlerBenchmarkCorpus(count int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, count*8)
	for i := 0; i < count; i++ {
		serviceAccountKey := fmt.Sprintf("sha256:sa-%d", i)
		subjectFingerprint := fmt.Sprintf("sha256:subject-%d", i)
		roleARN := fmt.Sprintf("arn:aws:iam::123456789012:role/svc-%d", i)
		policyKey := fmt.Sprintf("sha256:policy-%d", i)
		mountKey := fmt.Sprintf("sha256:mount-%d", i)
		kvPathFingerprint := fmt.Sprintf("sha256:kv-%d", i)

		envelopes = append(envelopes,
			secretsIAMReducerFact(fmt.Sprintf("sa-%d", i), facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
				"provider":                 "kubernetes",
				"service_account_join_key": serviceAccountKey,
			}),
			secretsIAMReducerFact(fmt.Sprintf("workload-%d", i), facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
				"provider":                 "kubernetes",
				"service_account_join_key": serviceAccountKey,
				"workload_object_id":       fmt.Sprintf("workload-%d", i),
				"workload_kind":            "deployments",
			}),
			secretsIAMReducerFact(fmt.Sprintf("irsa-%d", i), facts.EKSIRSAAnnotationFactKind, "k8s-scope", "k8s-gen", map[string]any{
				"provider":                         "kubernetes",
				"service_account_join_key":         serviceAccountKey,
				"role_arn":                         roleARN,
				"web_identity_subject_fingerprint": subjectFingerprint,
			}),
			secretsIAMReducerFact(fmt.Sprintf("principal-%d", i), facts.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
				"provider":       "aws_iam",
				"principal_arn":  roleARN,
				"principal_type": "aws_iam_role",
				"account_id":     "123456789012",
				"region":         "us-east-1",
			}),
			secretsIAMReducerFact(fmt.Sprintf("trust-%d", i), facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
				"provider":                          "aws_iam",
				"role_arn":                          roleARN,
				"effect":                            "Allow",
				"actions":                           []string{"sts:assumerolewithwebidentity"},
				"web_identity_subject_fingerprints": []string{subjectFingerprint},
				"web_identity_subject_wildcard":     false,
			}),
			secretsIAMReducerFact(fmt.Sprintf("vault-role-%d", i), facts.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
				"provider":                        "vault",
				"auth_method":                     "kubernetes",
				"role_join_key":                   fmt.Sprintf("sha256:vault-role-%d", i),
				"mount_join_key":                  mountKey,
				"bound_service_account_join_keys": []string{serviceAccountKey},
				"token_policy_join_keys":          []string{policyKey},
			}),
			secretsIAMReducerFact(fmt.Sprintf("vault-policy-%d", i), facts.VaultACLPolicyFactKind, "vault-scope", "vault-gen", map[string]any{
				"provider":        "vault",
				"policy_join_key": policyKey,
				// A multi-rule array: the []VaultACLPolicyRule nested-struct
				// shape the caveat identifies as the decode_map.go marshal
				// -fallback risk (Wave 4b's map[string]string and Wave 4c's
				// float64 gaps were both found via a nested/typed field no
				// prior family's benchmark exercised).
				"rules": []map[string]any{
					{
						"path_fingerprint": kvPathFingerprint,
						"path_depth":       3,
						"capabilities":     []string{"read"},
					},
					{
						"path_fingerprint": fmt.Sprintf("sha256:kv-secondary-%d", i),
						"path_depth":       4,
						"capabilities":     []string{"list"},
					},
				},
			}),
			secretsIAMReducerFact(fmt.Sprintf("vault-kv-%d", i), facts.VaultKVMetadataFactKind, "vault-scope", "vault-gen", map[string]any{
				"provider":            "vault",
				"mount_join_key":      mountKey,
				"kv_path_fingerprint": kvPathFingerprint,
				"path_depth":          3,
			}),
		)
	}
	return envelopes
}

// BenchmarkBuildSecretsIAMTrustChainReadModels measures the full
// SecretsIAMTrustChainHandler.Handle in-memory cost --
// BuildSecretsIAMTrustChainReadModels, which buildSecretsIAMIndex (now typed
// decode for every VAULT + K8S lane kind) and secretsIAMExactChains dominate
// -- against a 2,000-service-account corpus (16,000 fact envelopes), each
// producing one full exact workload-to-vault-secret chain plus one secret
// access path.
func BenchmarkBuildSecretsIAMTrustChainReadModels(b *testing.B) {
	const serviceAccountCount = 2000
	envelopes := buildSecretsIAMHandlerBenchmarkCorpus(serviceAccountCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		models, quarantined, err := BuildSecretsIAMTrustChainReadModels(envelopes)
		if err != nil {
			b.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
		}
		if len(quarantined) != 0 {
			b.Fatalf("quarantined = %d, want 0 for an all-valid corpus", len(quarantined))
		}
		if got, want := len(models.IdentityTrustChains), serviceAccountCount; got != want {
			b.Fatalf("IdentityTrustChains = %d, want %d", got, want)
		}
	}
}
