// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// iamRoleUIDChainEnvelopes builds an exact IRSA chain whose IAM principal fact
// optionally carries the AWS scan boundary (account_id/region). When both are
// present the build can resolve the CloudResource-joinable IAM-role uid; when
// they are absent the field must stay blank so the graph edge remains
// skipped+counted (ADR #1314 §5.1).
func iamRoleUIDChainEnvelopes(serviceAccountKey, subjectFingerprint, roleARN, accountID, region string) []facts.Envelope {
	principal := map[string]any{
		"provider":       "aws_iam",
		"principal_arn":  roleARN,
		"principal_type": "aws_iam_role",
	}
	if accountID != "" {
		principal["account_id"] = accountID
	}
	if region != "" {
		principal["region"] = region
	}
	return []facts.Envelope{
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
		secretsIAMReducerFact("principal", facts.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", principal),
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
			"mount_join_key":                  "sha256:vault-mount",
			"bound_service_account_join_keys": []string{serviceAccountKey},
			"token_policy_join_keys":          []string{"sha256:vault-policy"},
		}),
	}
}

func TestBuildSecretsIAMTrustChainReadModelsResolvesIAMRoleCloudResourceUID(t *testing.T) {
	t.Parallel()

	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	accountID := "123456789012"
	region := "aws-global"
	models := BuildSecretsIAMTrustChainReadModels(
		iamRoleUIDChainEnvelopes("sha256:sa-checkout-payments", "sha256:web-identity-subject", roleARN, accountID, region),
	)

	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d", got, want)
	}
	chain := models.IdentityTrustChains[0]
	wantUID := cloudResourceUID(accountID, region, "aws_iam_role", roleARN)
	if chain.IAMRoleCloudResourceUID != wantUID {
		t.Fatalf("chain.IAMRoleCloudResourceUID = %q, want %q", chain.IAMRoleCloudResourceUID, wantUID)
	}
	if chain.IAMRoleCloudResourceUID == roleARN {
		t.Fatal("chain leaked raw role ARN as the CloudResource uid")
	}
	if chain.IAMRoleAssumeMode != secretsIAMAssumeModeWebIdentity {
		t.Fatalf("chain.IAMRoleAssumeMode = %q, want web_identity", chain.IAMRoleAssumeMode)
	}
}

func TestBuildSecretsIAMTrustChainReadModelsResolvesPodIdentityAssumeMode(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-pod-identity"
	roleARN := "arn:aws:iam::123456789012:role/pod-identity"
	accountID := "123456789012"
	region := "aws-global"
	envelopes := []facts.Envelope{
		secretsIAMReducerFact("sa", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
		}),
		secretsIAMReducerFact("podidentity", facts.EKSPodIdentityAssociationFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"role_arn":                 roleARN,
			"association_id":           "assoc-1",
		}),
		secretsIAMReducerFact("principal", facts.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":       "aws_iam",
			"principal_arn":  roleARN,
			"principal_type": "aws_iam_role",
			"account_id":     accountID,
			"region":         region,
		}),
		secretsIAMReducerFact("trust", facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":          "aws_iam",
			"role_arn":          roleARN,
			"effect":            "Allow",
			"actions":           []string{"sts:assumerole"},
			"assume_principals": []string{"pods.eks.amazonaws.com"},
		}),
		secretsIAMReducerFact("vault-role", facts.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"role_join_key":                   "sha256:vault-role",
			"bound_service_account_join_keys": []string{serviceAccountKey},
		}),
	}

	models := BuildSecretsIAMTrustChainReadModels(envelopes)
	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d", got, want)
	}
	chain := models.IdentityTrustChains[0]
	if chain.IAMRoleCloudResourceUID != cloudResourceUID(accountID, region, "aws_iam_role", roleARN) {
		t.Fatalf("pod-identity chain did not resolve the IAM-role uid: %q", chain.IAMRoleCloudResourceUID)
	}
	if chain.IAMRoleAssumeMode != secretsIAMAssumeModePodIdentity {
		t.Fatalf("chain.IAMRoleAssumeMode = %q, want pod_identity", chain.IAMRoleAssumeMode)
	}
}

func TestBuildSecretsIAMTrustChainReadModelsLeavesIAMRoleUIDBlankWithoutAccountRegion(t *testing.T) {
	t.Parallel()

	// Without the IAM principal fact's account_id/region the build cannot resolve
	// a CloudResource-joinable uid, so the field stays blank and the graph edge
	// remains skipped+counted downstream (no fabricated join).
	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	models := BuildSecretsIAMTrustChainReadModels(
		iamRoleUIDChainEnvelopes("sha256:sa-checkout-payments", "sha256:web-identity-subject", roleARN, "", ""),
	)

	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d", got, want)
	}
	if got := models.IdentityTrustChains[0].IAMRoleCloudResourceUID; got != "" {
		t.Fatalf("IAMRoleCloudResourceUID = %q, want blank without account/region", got)
	}
}
