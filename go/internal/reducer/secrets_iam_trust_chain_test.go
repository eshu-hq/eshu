// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSecretsIAMTrustChainReadModelsAdmitsExactWorkloadToVaultPath(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	subjectFingerprint := "sha256:web-identity-subject"
	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	policyKey := "sha256:vault-policy"
	mountKey := "sha256:vault-mount"
	kvPathFingerprint := "sha256:kv-path"
	envelopes := []facts.Envelope{
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

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d: %#v", got, want, models.IdentityTrustChains)
	}
	chain := models.IdentityTrustChains[0]
	if got, want := chain.State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("chain.State = %q, want %q", got, want)
	}
	if chain.IAMRoleFingerprint == "" {
		t.Fatal("chain.IAMRoleFingerprint is blank")
	}
	if chain.IAMRoleFingerprint == roleARN {
		t.Fatal("chain leaked raw role ARN")
	}
	if got, want := chain.WorkloadObjectID, "workload-stable-id"; got != want {
		t.Fatalf("chain.WorkloadObjectID = %q, want %q", got, want)
	}
	if got, want := len(models.SecretAccessPaths), 1; got != want {
		t.Fatalf("SecretAccessPaths len = %d, want %d: %#v", got, want, models.SecretAccessPaths)
	}
	path := models.SecretAccessPaths[0]
	if got, want := path.State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("path.State = %q, want %q", got, want)
	}
	if got, want := path.KVPathFingerprint, kvPathFingerprint; got != want {
		t.Fatalf("path.KVPathFingerprint = %q, want %q", got, want)
	}
}

func TestBuildSecretsIAMTrustChainReadModelsRejectsNameCoincidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		secretsIAMReducerFact("sa", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": "sha256:sa-a",
		}),
		secretsIAMReducerFact("vault-role", facts.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"role_join_key":                   "sha256:vault-role",
			"bound_service_account_join_keys": []string{"sha256:sa-b"},
		}),
	}

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
	for _, chain := range models.IdentityTrustChains {
		if chain.State == SecretsIAMTrustChainStateExact {
			t.Fatalf("unexpected exact chain from unrelated service accounts: %#v", chain)
		}
	}
	if got := len(models.SecretAccessPaths); got != 0 {
		t.Fatalf("SecretAccessPaths len = %d, want 0 without exact service-account join", got)
	}
}

func TestBuildSecretsIAMTrustChainReadModelsKeepsWildcardTrustAsPostureEvidence(t *testing.T) {
	t.Parallel()

	roleARN := "arn:aws:iam::123456789012:role/broad"
	envelopes := []facts.Envelope{
		secretsIAMReducerFact("trust", facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":                      "aws_iam",
			"role_arn":                      roleARN,
			"effect":                        "Allow",
			"actions":                       []string{"sts:assumerolewithwebidentity"},
			"web_identity_subject_wildcard": true,
		}),
	}

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
	if got := len(models.IdentityTrustChains); got != 0 {
		t.Fatalf("IdentityTrustChains len = %d, want 0 for wildcard-only trust", got)
	}
	observation := secretsIAMPostureObservationByRisk(t, models, "wildcard_web_identity_subject")
	if got, want := observation.State, SecretsIAMTrustChainStatePartial; got != want {
		t.Fatalf("observation.State = %q, want %q", got, want)
	}
	if observation.SubjectFingerprint == "" || observation.SubjectFingerprint == roleARN {
		t.Fatalf("SubjectFingerprint = %q, want redacted role fingerprint", observation.SubjectFingerprint)
	}
}

func TestBuildSecretsIAMTrustChainReadModelsIgnoresDenyWildcardTrustPosture(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		secretsIAMReducerFact("trust", facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":                      "aws_iam",
			"role_arn":                      "arn:aws:iam::123456789012:role/denied",
			"effect":                        "Deny",
			"actions":                       []string{"sts:AssumeRoleWithWebIdentity"},
			"web_identity_subject_wildcard": true,
		}),
	}

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
	if got := len(models.PrivilegePostureObservations); got != 0 {
		t.Fatalf("PrivilegePostureObservations len = %d, want 0 for Deny-only wildcard trust", got)
	}
}

func TestBuildSecretsIAMTrustChainReadModelsRejectsWildcardVaultSelector(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	roleARN := "arn:aws:iam::123456789012:role/payments-pod-identity"
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
		secretsIAMReducerFact("pod-identity", facts.EKSPodIdentityAssociationFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"role_arn":                 roleARN,
		}),
		secretsIAMReducerFact("principal", facts.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":       "aws_iam",
			"principal_arn":  roleARN,
			"principal_type": "aws_iam_role",
			"account_id":     "123456789012",
			"region":         "us-east-1",
		}),
		secretsIAMReducerFact("trust", facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":          "aws_iam",
			"role_arn":          roleARN,
			"effect":            "Allow",
			"actions":           []string{"sts:AssumeRole"},
			"assume_principals": []string{"pods.eks.amazonaws.com"},
		}),
		secretsIAMReducerFact("vault-role", facts.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"role_join_key":                   "sha256:vault-role",
			"bound_service_account_join_keys": []string{serviceAccountKey},
			"bound_service_account_selector_wildcard": true,
		}),
	}

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
	if got := len(models.IdentityTrustChains); got != 0 {
		t.Fatalf("IdentityTrustChains len = %d, want 0 for wildcard Vault selector", got)
	}
	observation := secretsIAMPostureObservationByRisk(t, models, "wildcard_vault_service_account_selector")
	if got, want := observation.State, SecretsIAMTrustChainStatePartial; got != want {
		t.Fatalf("observation.State = %q, want %q", got, want)
	}
}

func TestBuildSecretsIAMTrustChainReadModelsAdmitsExactEKSPodIdentity(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	roleARN := "arn:aws:iam::123456789012:role/payments-pod-identity"
	policyKey := "sha256:vault-policy"
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
		secretsIAMReducerFact("pod-identity", facts.EKSPodIdentityAssociationFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"role_arn":                 roleARN,
		}),
		secretsIAMReducerFact("principal", facts.AWSIAMPrincipalFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":       "aws_iam",
			"principal_arn":  roleARN,
			"principal_type": "aws_iam_role",
			"account_id":     "123456789012",
			"region":         "us-east-1",
		}),
		secretsIAMReducerFact("trust", facts.AWSIAMTrustPolicyFactKind, "aws-scope", "aws-gen", map[string]any{
			"provider":          "aws_iam",
			"role_arn":          roleARN,
			"effect":            "Allow",
			"actions":           []string{"sts:AssumeRole"},
			"assume_principals": []string{"pods.eks.amazonaws.com"},
		}),
		secretsIAMReducerFact("vault-role", facts.VaultAuthRoleFactKind, "vault-scope", "vault-gen", map[string]any{
			"provider":                        "vault",
			"auth_method":                     "kubernetes",
			"role_join_key":                   "sha256:vault-role",
			"bound_service_account_join_keys": []string{serviceAccountKey},
			"token_policy_join_keys":          []string{policyKey},
		}),
	}

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d: %#v", got, want, models.IdentityTrustChains)
	}
	if got, want := models.IdentityTrustChains[0].State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("chain.State = %q, want %q", got, want)
	}
}

func TestBuildSecretsIAMTrustChainReadModelsEmitsStaleGenerationGap(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:sa-checkout-payments"
	envelopes := []facts.Envelope{
		secretsIAMReducerFact("sa", facts.KubernetesServiceAccountFactKind, "k8s-scope", "new-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "old-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
		}),
	}

	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}
	gap := secretsIAMPostureGapByType(t, models, "stale_generation")
	if got, want := gap.State, SecretsIAMTrustChainStateStale; got != want {
		t.Fatalf("gap.State = %q, want %q", got, want)
	}
}

func TestSecretsIAMTrustChainHandlerLoadsEvidenceAndWritesReadModels(t *testing.T) {
	t.Parallel()

	loader := &recordingSecretsIAMTrustChainEvidenceLoader{
		envelopes: []facts.Envelope{
			secretsIAMReducerFact("coverage", facts.SecretsIAMCoverageWarningFactKind, "aws-scope", "aws-gen", map[string]any{
				"provider":       "aws_iam",
				"warning_kind":   "scp_uncollected",
				"source_state":   "unsupported",
				"resource_scope": "aws_organizations",
			}),
		},
		stats: SecretsIAMTrustChainLoadStats{SeedFactCount: 1, LoadedFactCount: 1},
	}
	writer := &recordingSecretsIAMTrustChainWriter{}
	handler := SecretsIAMTrustChainHandler{EvidenceLoader: loader, Writer: writer}
	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "aws-scope",
		GenerationID: "aws-gen",
		Domain:       DomainSecretsIAMTrustChain,
		SourceSystem: "secrets_iam_posture",
		Cause:        "secrets/IAM source facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := result.Domain, DomainSecretsIAMTrustChain; got != want {
		t.Fatalf("result.Domain = %q, want %q", got, want)
	}
	if got, want := writer.calls, 1; got != want {
		t.Fatalf("writer calls = %d, want %d", got, want)
	}
	if got := len(writer.write.Models.PostureGaps); got != 1 {
		t.Fatalf("PostureGaps len = %d, want 1", got)
	}
}

func secretsIAMReducerFact(
	factID string,
	factKind string,
	scopeID string,
	generationID string,
	payload map[string]any,
) facts.Envelope {
	version, _ := facts.SecretsIAMSchemaVersion(factKind)
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      factKind,
		SchemaVersion: version,
		CollectorKind: "secrets_iam_posture",
		ObservedAt:    time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
		Payload:       payload,
		SourceRef: facts.Ref{
			SourceSystem: "secrets_iam_posture",
			ScopeID:      scopeID,
			GenerationID: generationID,
			FactKey:      factID,
		},
	}
}

func secretsIAMPostureObservationByRisk(
	t *testing.T,
	models SecretsIAMTrustChainReadModels,
	riskType string,
) SecretsIAMPrivilegePostureObservation {
	t.Helper()
	for _, observation := range models.PrivilegePostureObservations {
		if observation.RiskType == riskType {
			return observation
		}
	}
	t.Fatalf("risk_type %q missing in %#v", riskType, models.PrivilegePostureObservations)
	return SecretsIAMPrivilegePostureObservation{}
}

func secretsIAMPostureGapByType(
	t *testing.T,
	models SecretsIAMTrustChainReadModels,
	gapType string,
) SecretsIAMPostureGap {
	t.Helper()
	for _, gap := range models.PostureGaps {
		if gap.GapType == gapType {
			return gap
		}
	}
	t.Fatalf("gap_type %q missing in %#v", gapType, models.PostureGaps)
	return SecretsIAMPostureGap{}
}

type recordingSecretsIAMTrustChainEvidenceLoader struct {
	envelopes []facts.Envelope
	stats     SecretsIAMTrustChainLoadStats
	intent    Intent
}

func (l *recordingSecretsIAMTrustChainEvidenceLoader) LoadSecretsIAMTrustChainEvidence(
	_ context.Context,
	intent Intent,
) ([]facts.Envelope, SecretsIAMTrustChainLoadStats, error) {
	l.intent = intent
	return l.envelopes, l.stats, nil
}

type recordingSecretsIAMTrustChainWriter struct {
	calls int
	write SecretsIAMTrustChainWrite
}

func (w *recordingSecretsIAMTrustChainWriter) WriteSecretsIAMTrustChainReadModels(
	_ context.Context,
	write SecretsIAMTrustChainWrite,
) (SecretsIAMTrustChainWriteResult, error) {
	w.calls++
	w.write = write
	return SecretsIAMTrustChainWriteResult{
		FactsWritten: len(write.Models.IdentityTrustChains) +
			len(write.Models.PrivilegePostureObservations) +
			len(write.Models.SecretAccessPaths) +
			len(write.Models.PostureGaps),
		EvidenceSummary: "recorded secrets/IAM trust-chain read models",
	}, nil
}
