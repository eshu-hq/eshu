// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
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
	models, err := BuildSecretsIAMTrustChainReadModels(
		iamRoleUIDChainEnvelopes("sha256:sa-checkout-payments", "sha256:web-identity-subject", roleARN, accountID, region),
	)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

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

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
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

// TestBuildSecretsIAMTrustChainReadModelsDeadLettersPrincipalMissingAccountID is
// the aws_iam_principal regression test mirroring the flagship: a principal fact
// whose required account_id key is ABSENT dead-letters as input_invalid rather
// than silently leaving the IAM-role uid blank. Before the typed-decode
// migration this returned a blank uid (a silent skip); now the malformed fact is
// a classified, non-retryable failure the secrets/IAM trust-chain work item
// surfaces.
//
// The iamRoleUIDChainEnvelopes helper omits account_id/region entirely when
// passed "" (absent, not empty-string present), which is the exact malformed
// shape the decode seam rejects.
func TestBuildSecretsIAMTrustChainReadModelsDeadLettersPrincipalMissingAccountID(t *testing.T) {
	t.Parallel()

	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	_, err := BuildSecretsIAMTrustChainReadModels(
		iamRoleUIDChainEnvelopes("sha256:sa-checkout-payments", "sha256:web-identity-subject", roleARN, "", ""),
	)
	if err == nil {
		t.Fatal("BuildSecretsIAMTrustChainReadModels() error = nil, want a dead-letter for an aws_iam_principal fact missing required account_id")
	}

	// The surfaced error must self-classify as input_invalid so the work item
	// dead-letters (non-retryable) rather than looping.
	var classified classifiedReducerFailure
	if !errors.As(err, &classified) {
		t.Fatalf("error %v (%T) does not implement FailureClass(); a decode failure must surface as a self-classifying reducer error", err, err)
	}
	if got := classified.FailureClass(); got != "input_invalid" {
		t.Fatalf("FailureClass() = %q, want %q", got, "input_invalid")
	}
	if IsRetryable(err) {
		t.Fatal("IsRetryable(err) = true for a missing-required-field decode failure; input_invalid is terminal")
	}
}

// TestBuildSecretsIAMTrustChainReadModelsLeavesIAMRoleUIDBlankWithEmptyAccountRegion
// proves the present-but-empty distinction: a principal fact whose account_id and
// region keys are PRESENT with empty-string values is a valid (if unusual)
// decode, and the build leaves the IAM-role uid blank exactly as before — only an
// absent key dead-letters, never an empty one.
func TestBuildSecretsIAMTrustChainReadModelsLeavesIAMRoleUIDBlankWithEmptyAccountRegion(t *testing.T) {
	t.Parallel()

	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	envelopes := iamRoleUIDChainEnvelopes("sha256:sa-checkout-payments", "sha256:web-identity-subject", roleARN, "", "")
	// Force the principal fact to carry account_id/region as present-but-empty
	// rather than absent, so decode succeeds and the blank-uid path is exercised.
	for i := range envelopes {
		if envelopes[i].FactKind == facts.AWSIAMPrincipalFactKind {
			envelopes[i].Payload["account_id"] = ""
			envelopes[i].Payload["region"] = ""
		}
	}

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil for present-but-empty account/region", err)
	}
	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d", got, want)
	}
	if got := models.IdentityTrustChains[0].IAMRoleCloudResourceUID; got != "" {
		t.Fatalf("IAMRoleCloudResourceUID = %q, want blank with empty account/region", got)
	}
}
