// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

func TestFactStoreLoadSecretsIAMTrustChainEvidenceExpandsActiveAnchors(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:service-account"
	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	policyKey := "sha256:vault-policy"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-sa",
					"k8s-scope",
					"k8s-gen",
					facts.KubernetesServiceAccountFactKind,
					`{"service_account_join_key":"sha256:service-account"}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-irsa",
					"k8s-scope",
					"k8s-gen",
					facts.EKSIRSAAnnotationFactKind,
					`{"service_account_join_key":"sha256:service-account","role_arn":"arn:aws:iam::123456789012:role/payments-api"}`,
				),
				secretsIAMTrustChainFactRow(
					"vault-role",
					"vault-scope",
					"vault-gen",
					facts.VaultAuthRoleFactKind,
					`{"role_join_key":"sha256:vault-role","bound_service_account_join_keys":["sha256:service-account"],"token_policy_join_keys":["sha256:vault-policy"]}`,
				),
			}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	envelopes, stats, err := store.LoadSecretsIAMTrustChainEvidence(context.Background(), reducer.Intent{
		ScopeID:      "k8s-scope",
		GenerationID: "k8s-gen",
		Domain:       reducer.DomainSecretsIAMTrustChain,
	})
	if err != nil {
		t.Fatalf("LoadSecretsIAMTrustChainEvidence() error = %v, want nil", err)
	}
	if got, want := stats.SeedFactCount, 1; got != want {
		t.Fatalf("SeedFactCount = %d, want %d", got, want)
	}
	if got, want := stats.LoadedFactCount, 3; got != want {
		t.Fatalf("LoadedFactCount = %d, want %d", got, want)
	}
	if got, want := len(envelopes), 3; got != want {
		t.Fatalf("len(envelopes) = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 3; got != want {
		t.Fatalf("QueryContext calls = %d, want %d", got, want)
	}
	activeQuery := db.queries[1].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.payload->>'service_account_join_key' = ANY($2::text[])",
		"fact.payload->'bound_service_account_join_keys' ?| $2::text[]",
		"fact.payload->>'role_arn' = ANY($3::text[])",
		"fact.payload->'token_policy_join_keys' ?| $5::text[]",
	} {
		if !strings.Contains(activeQuery, want) {
			t.Fatalf("active query missing %q:\n%s", want, activeQuery)
		}
	}
	if !slices.Contains(stringsArg(t, db.queries[1].args[1]), serviceAccountKey) {
		t.Fatalf("first active expansion missing service account anchor: %#v", db.queries[1].args[1])
	}
	if !slices.Contains(stringsArg(t, db.queries[2].args[2]), roleARN) {
		t.Fatalf("second active expansion missing role ARN anchor: %#v", db.queries[2].args[2])
	}
	if !slices.Contains(stringsArg(t, db.queries[2].args[4]), policyKey) {
		t.Fatalf("second active expansion missing policy anchor: %#v", db.queries[2].args[4])
	}
}

func TestFactStoreLoadSecretsIAMTrustChainEvidenceExpandsGCPWorkloadIdentityAnchors(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:service-account"
	emailDigest := "sha256:gcp-service-account-email"
	subjectFingerprint := "sha256:gke-subject"
	targetPrincipalFingerprint := "sha256:gcp-service-account"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-gcp-binding",
					"k8s-scope",
					"k8s-gen",
					facts.KubernetesGCPWorkloadIdentityBindingFactKind,
					`{"service_account_join_key":"sha256:service-account","gcp_service_account_email_digest":"sha256:gcp-service-account-email","gcp_workload_identity_subject_fingerprint":"sha256:gke-subject"}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"gcp-trust",
					"gcp-scope",
					"gcp-gen",
					facts.GCPIAMTrustPolicyFactKind,
					`{"provider":"gcp","collector_instance_id":"collector-redacted","redaction_policy_version":"test-v1","target_principal_fingerprint":"sha256:gcp-service-account","target_service_account_email_digest":"sha256:gcp-service-account-email","role":"roles/iam.workloadIdentityUser","impersonation_mode":"workload_identity","gcp_workload_identity_subject_fingerprint":"sha256:gke-subject"}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"gcp-principal",
					"gcp-scope",
					"gcp-gen",
					facts.GCPIAMPrincipalFactKind,
					`{"provider":"gcp","collector_instance_id":"collector-redacted","redaction_policy_version":"test-v1","principal_fingerprint":"sha256:gcp-service-account","principal_type":"service_account"}`,
				),
				secretsIAMTrustChainFactRow(
					"gcp-permission",
					"gcp-scope",
					"gcp-gen",
					facts.GCPIAMPermissionPolicyFactKind,
					`{"provider":"gcp","collector_instance_id":"collector-redacted","redaction_policy_version":"test-v1","principal_fingerprint":"sha256:gcp-service-account","principal_type":"service_account","role":"roles/secretmanager.secretAccessor","resource_full_resource_name":"//secretmanager.googleapis.com/projects/redacted/secrets/redacted","resource_is_secret":true}`,
				),
			}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	envelopes, stats, err := store.LoadSecretsIAMTrustChainEvidence(context.Background(), reducer.Intent{
		ScopeID:      "k8s-scope",
		GenerationID: "k8s-gen",
		Domain:       reducer.DomainSecretsIAMTrustChain,
	})
	if err != nil {
		t.Fatalf("LoadSecretsIAMTrustChainEvidence() error = %v, want nil", err)
	}
	if got, want := stats.LoadedFactCount, 4; got != want {
		t.Fatalf("LoadedFactCount = %d, want %d; envelopes=%#v", got, want, envelopes)
	}
	activeQuery := db.queries[1].query
	for _, want := range []string{
		"fact.payload->>'gcp_workload_identity_subject_fingerprint' = ANY($4::text[])",
		"fact.payload->>'target_principal_fingerprint' = ANY($7::text[])",
		"fact.payload->>'gcp_service_account_email_digest' = ANY($8::text[])",
		"fact.payload->>'target_service_account_email_digest' = ANY($8::text[])",
	} {
		if !strings.Contains(activeQuery, want) {
			t.Fatalf("active query missing %q:\n%s", want, activeQuery)
		}
	}
	if !slices.Contains(stringsArg(t, db.queries[1].args[1]), serviceAccountKey) {
		t.Fatalf("first active expansion missing service account anchor: %#v", db.queries[1].args[1])
	}
	if !slices.Contains(stringsArg(t, db.queries[1].args[3]), subjectFingerprint) {
		t.Fatalf("first active expansion missing GCP subject anchor: %#v", db.queries[1].args[3])
	}
	if !slices.Contains(stringsArg(t, db.queries[1].args[7]), emailDigest) {
		t.Fatalf("first active expansion missing GCP service-account email digest: %#v", db.queries[1].args[7])
	}
	if !slices.Contains(stringsArg(t, db.queries[2].args[6]), targetPrincipalFingerprint) {
		t.Fatalf("second active expansion missing target principal fingerprint: %#v", db.queries[2].args[6])
	}
}

func TestFactStoreLoadSecretsIAMTrustChainEvidenceClassifiesMalformedAnchor(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"aws-trust",
					"aws-scope",
					"aws-gen",
					facts.AWSIAMTrustPolicyFactKind,
					`{"region":"global","provider":"aws","collector_instance_id":"collector-redacted","redaction_policy_version":"test-v1","role_arn":"arn:aws:iam::000000000000:role/redacted","policy_source":"assume_role_policy","effect":"Allow"}`,
				),
			}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	_, _, err := store.LoadSecretsIAMTrustChainEvidence(context.Background(), reducer.Intent{
		ScopeID:      "aws-scope",
		GenerationID: "aws-gen",
		Domain:       reducer.DomainSecretsIAMTrustChain,
	})
	if err == nil {
		t.Fatal("LoadSecretsIAMTrustChainEvidence() error = nil, want classified input_invalid decode error")
	}
	var decodeErr *factschema.DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("LoadSecretsIAMTrustChainEvidence() error = %T, want *factschema.DecodeError", err)
	}
	if decodeErr.Classification != factschema.ClassificationInputInvalid {
		t.Fatalf("DecodeError.Classification = %q, want %q", decodeErr.Classification, factschema.ClassificationInputInvalid)
	}
	if decodeErr.Field != "account_id" {
		t.Fatalf("DecodeError.Field = %q, want account_id", decodeErr.Field)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("LoadSecretsIAMTrustChainEvidence() error = %T, want FailureClass", err)
	}
	if classified.FailureClass() != factschema.ClassificationInputInvalid {
		t.Fatalf("FailureClass() = %q, want %q", classified.FailureClass(), factschema.ClassificationInputInvalid)
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) {
		t.Fatalf("LoadSecretsIAMTrustChainEvidence() error = %T, want Retryable", err)
	}
	if retryable.Retryable() {
		t.Fatal("Retryable() = true, want false for malformed contract payload")
	}
}

func TestFactStoreLoadSecretsIAMTrustChainEvidenceMarksTruncatedAtExpansionLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-sa",
					"k8s-scope",
					"k8s-gen",
					facts.KubernetesServiceAccountFactKind,
					`{"service_account_join_key":"sha256:service-account"}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-irsa",
					"k8s-scope",
					"k8s-gen",
					facts.EKSIRSAAnnotationFactKind,
					`{"service_account_join_key":"sha256:service-account","role_arn":"arn:aws:iam::123456789012:role/payments-api"}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"vault-role",
					"vault-scope",
					"vault-gen",
					facts.VaultAuthRoleFactKind,
					`{"role_join_key":"sha256:vault-role","bound_service_account_join_keys":["sha256:service-account"],"token_policy_join_keys":["sha256:vault-policy"]}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"vault-policy",
					"vault-scope",
					"vault-gen",
					facts.VaultACLPolicyFactKind,
					`{"policy_join_key":"sha256:vault-policy","rules":[{"path_fingerprint":"sha256:kv-path"}]}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"vault-kv",
					"vault-scope",
					"vault-gen",
					facts.VaultKVMetadataFactKind,
					`{"mount_join_key":"sha256:vault-mount","kv_path_fingerprint":"sha256:kv-path"}`,
				),
			}},
		},
	}
	store := NewFactStore(db)

	_, stats, err := store.LoadSecretsIAMTrustChainEvidence(context.Background(), reducer.Intent{
		ScopeID:      "k8s-scope",
		GenerationID: "k8s-gen",
		Domain:       reducer.DomainSecretsIAMTrustChain,
	})
	if err != nil {
		t.Fatalf("LoadSecretsIAMTrustChainEvidence() error = %v, want nil", err)
	}
	if !stats.Truncated {
		t.Fatal("Truncated = false, want true after expansion limit adds new anchors")
	}
	if got, want := len(db.queries), secretsIAMTrustChainMaxExpansionPasses+1; got != want {
		t.Fatalf("QueryContext calls = %d, want %d", got, want)
	}
}

func secretsIAMTrustChainFactRow(
	factID string,
	scopeID string,
	generationID string,
	factKind string,
	payload string,
) []any {
	version, _ := facts.SecretsIAMSchemaVersion(factKind)
	return []any{
		factID,
		scopeID,
		generationID,
		factKind,
		factKind + ":" + factID,
		version,
		"secrets_iam_posture",
		int64(0),
		"reported",
		"secrets_iam_posture",
		factID,
		"",
		factID,
		time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
		false,
		[]byte(payload),
	}
}

func stringsArg(t *testing.T, arg any) []string {
	t.Helper()
	values, ok := arg.([]string)
	if !ok {
		t.Fatalf("arg type = %T, want []string", arg)
	}
	return values
}
