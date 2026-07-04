// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
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
	models, _, err := BuildSecretsIAMTrustChainReadModels(
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

	models, _, err := BuildSecretsIAMTrustChainReadModels(envelopes)
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

// secretsIAMHasGap reports whether the read models carry a posture gap of the
// given type.
func secretsIAMHasGap(models SecretsIAMTrustChainReadModels, gapType string) bool {
	for _, gap := range models.PostureGaps {
		if gap.GapType == gapType {
			return true
		}
	}
	return false
}

// TestBuildSecretsIAMTrustChainReadModelsQuarantinesPrincipalMissingAccountID is
// the aws_iam_principal regression test mirroring the flagship, updated to the
// per-fact isolation contract every migrated reducer kind now follows. A
// principal fact whose required account_id key is ABSENT is QUARANTINED as an
// input_invalid per-fact dead-letter (returned in the []quarantinedFact slice)
// rather than aborting the whole trust-chain work item. Because the malformed
// principal never enters the index, the role has no valid principal, so the
// build emits a missing_iam_principal posture gap for it (its existing
// no-principal path) instead of a chain — the correct, visible outcome, not a
// silent blank-uid chain. Build returns nil so the scope's other providers'
// chains still project.
//
// The iamRoleUIDChainEnvelopes helper omits account_id/region entirely when
// passed "" (absent, not empty-string present), which is the exact malformed
// shape the decode seam rejects.
func TestBuildSecretsIAMTrustChainReadModelsQuarantinesPrincipalMissingAccountID(t *testing.T) {
	t.Parallel()

	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	models, quarantined, err := BuildSecretsIAMTrustChainReadModels(
		iamRoleUIDChainEnvelopes("sha256:sa-checkout-payments", "sha256:web-identity-subject", roleARN, "", ""),
	)
	// Per-fact isolation: the malformed principal must not abort the work item.
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil (per-fact isolation, not batch abort)", err)
	}

	// The malformed principal must be quarantined exactly once, classified as
	// input_invalid on the missing account_id field.
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-account_id principal must be quarantined once", len(quarantined))
	}
	if quarantined[0].factKind != facts.AWSIAMPrincipalFactKind {
		t.Fatalf("quarantined factKind = %q, want %q", quarantined[0].factKind, facts.AWSIAMPrincipalFactKind)
	}
	if quarantined[0].field != "account_id" {
		t.Fatalf("quarantined field = %q, want %q", quarantined[0].field, "account_id")
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined classification = %q, want %q", quarantined[0].classification, "input_invalid")
	}

	// With its only principal quarantined, the role resolves no valid principal,
	// so the build emits a missing_iam_principal gap rather than a chain: no chain
	// is ever resolved against a zero-value identity.
	if len(models.IdentityTrustChains) != 0 {
		t.Fatalf("IdentityTrustChains len = %d, want 0; a role whose lone principal was quarantined must not form a chain", len(models.IdentityTrustChains))
	}
	if !secretsIAMHasGap(models, "missing_iam_principal") {
		t.Fatal("want a missing_iam_principal posture gap for the role whose only principal was quarantined")
	}
}

// TestBuildSecretsIAMTrustChainReadModelsIsolatesMalformedPrincipalFromValidChains
// proves the isolation half of the contract: a malformed aws_iam_principal for
// one service account's role does not suppress a VALID chain for a different
// service account whose principal decodes cleanly. The malformed role produces a
// missing_iam_principal gap (no chain); the valid role's chain projects and
// resolves its uid fully.
func TestBuildSecretsIAMTrustChainReadModelsIsolatesMalformedPrincipalFromValidChains(t *testing.T) {
	t.Parallel()

	badRoleARN := "arn:aws:iam::123456789012:role/bad"
	goodRoleARN := "arn:aws:iam::123456789012:role/good"
	accountID := "123456789012"
	region := "aws-global"

	envelopes := iamRoleUIDChainEnvelopes("sha256:sa-bad", "sha256:subject-bad", badRoleARN, "", "")
	envelopes = append(
		envelopes,
		iamRoleUIDChainEnvelopes("sha256:sa-good", "sha256:subject-good", goodRoleARN, accountID, region)...,
	)

	models, quarantined, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; only the malformed principal is quarantined", len(quarantined))
	}

	// Exactly the valid chain projects; the malformed role forms none.
	wantGoodUID := cloudResourceUID(accountID, region, "aws_iam_role", goodRoleARN)
	goodFingerprint := secretsIAMFingerprint("iam_role", goodRoleARN)
	badFingerprint := secretsIAMFingerprint("iam_role", badRoleARN)
	var sawGood bool
	for _, chain := range models.IdentityTrustChains {
		if chain.IAMRoleFingerprint == badFingerprint {
			t.Fatalf("a chain formed for the role whose principal was quarantined: uid=%q", chain.IAMRoleCloudResourceUID)
		}
		if chain.IAMRoleFingerprint == goodFingerprint {
			sawGood = true
			if chain.IAMRoleCloudResourceUID != wantGoodUID {
				t.Fatalf("valid chain uid = %q, want %q; a malformed sibling must not suppress a valid chain", chain.IAMRoleCloudResourceUID, wantGoodUID)
			}
		}
	}
	if !sawGood {
		t.Fatal("valid chain did not project; isolation must keep valid chains intact")
	}
	if !secretsIAMHasGap(models, "missing_iam_principal") {
		t.Fatal("want a missing_iam_principal gap for the role whose principal was quarantined")
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

	models, _, err := BuildSecretsIAMTrustChainReadModels(envelopes)
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

// TestSecretsIAMPrincipalDecodeFatalStaysFatal locks the fatal-passthrough
// contract the aws_iam_principal branch in buildSecretsIAMIndex relies on. The
// secrets trust-chain build is the ONE decode call site whose extractor
// (buildSecretsIAMIndex) could not originally propagate a decode error, so its
// branch must return the fatal partitionDecodeFailures reports rather than
// swallow it. Today every factschema decode error is classified input_invalid
// (so the fatal branch is defensive/unreachable through the production seam),
// but if the seam ever adds a non-input_invalid classification, the branch's
// `return secretsIAMIndex{}, nil, fatal` must fire — this test proves the exact
// mechanism it depends on: a *factDecodeError whose classification is NOT
// input_invalid is returned FATAL by partitionDecodeFailures, not quarantined.
// TestPartitionDecodeFailures covers the classifier generically; this co-located
// test documents the contract for the 16 families that copy the secrets pattern.
func TestSecretsIAMPrincipalDecodeFatalStaysFatal(t *testing.T) {
	t.Parallel()

	principalEnv := facts.Envelope{FactID: "principal-bad", FactKind: facts.AWSIAMPrincipalFactKind}
	// A *factDecodeError classified as something OTHER than input_invalid — the
	// class the secrets branch must treat as fatal, not quarantine.
	fatalDecode := newFactDecodeError(factschema.FactKindAWSIAMPrincipal, &factschema.DecodeError{
		FactKind:       factschema.FactKindAWSIAMPrincipal,
		Classification: "schema_mismatch",
	})

	q, ok, fatal := partitionDecodeFailures(principalEnv, fatalDecode)
	if ok {
		t.Fatal("ok = true; a non-input_invalid principal decode error must NOT be quarantined by the secrets branch")
	}
	if fatal == nil {
		t.Fatal("fatal = nil; a non-input_invalid principal decode error must stay fatal so the trust-chain work item fails")
	}
	if (q != quarantinedFact{}) {
		t.Fatalf("quarantinedFact = %+v, want zero value for a fatal principal error", q)
	}
}
