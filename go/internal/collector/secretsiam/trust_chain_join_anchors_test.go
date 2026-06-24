// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestEKSIRSAAnnotationEnvelopeEmitsSubjectFingerprintOnly(t *testing.T) {
	t.Parallel()

	envelope, err := NewEKSIRSAAnnotationEnvelope(EKSIRSAAnnotationObservation{
		Context: KubernetesContext{
			ClusterID:           "cluster-a",
			ScopeID:             "scope-k8s",
			GenerationID:        "generation-k8s",
			CollectorInstanceID: "collector-k8s",
			FencingToken:        7,
			ObservedAt:          time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
		},
		Namespace:          "checkout",
		ServiceAccountName: "payments-api",
		ServiceAccountUID:  "uid-1",
		RoleARN:            "arn:aws:iam::123456789012:role/payments-api",
		AnnotationPresent:  true,
	})
	if err != nil {
		t.Fatalf("NewEKSIRSAAnnotationEnvelope() error = %v, want nil", err)
	}

	fingerprint, ok := envelope.Payload["web_identity_subject_fingerprint"].(string)
	if !ok || fingerprint == "" {
		t.Fatalf("web_identity_subject_fingerprint = %#v, want non-empty string", envelope.Payload["web_identity_subject_fingerprint"])
	}
	for _, forbidden := range []string{"checkout", "payments-api", "system:serviceaccount:checkout:payments-api"} {
		if fingerprint == forbidden {
			t.Fatalf("web identity subject fingerprint leaked raw subject %q", forbidden)
		}
	}
}

func TestVaultAuthRoleEnvelopeEmitsExactServiceAccountJoinKeysOnly(t *testing.T) {
	t.Parallel()

	envelope, err := NewVaultAuthRoleEnvelope(VaultAuthRoleObservation{
		Context: VaultContext{
			VaultClusterID:      "vault-a",
			Namespace:           "admin",
			ScopeID:             "scope-vault",
			GenerationID:        "generation-vault",
			CollectorInstanceID: "collector-vault",
			FencingToken:        9,
			ObservedAt:          time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
			RedactionKey:        testSecretsIAMRedactionKey(),
		},
		MountPath:                     "auth/kubernetes",
		RoleName:                      "payments-api",
		AuthMethod:                    VaultAuthMethodKubernetes,
		KubernetesClusterID:           "cluster-a",
		BoundServiceAccountNames:      []string{"payments-api"},
		BoundServiceAccountNamespaces: []string{"checkout"},
		TokenPolicyNames:              []string{"kv-read"},
	})
	if err != nil {
		t.Fatalf("NewVaultAuthRoleEnvelope() error = %v, want nil", err)
	}
	joinKeys, ok := envelope.Payload["bound_service_account_join_keys"].([]string)
	if !ok || len(joinKeys) != 1 || joinKeys[0] == "" {
		t.Fatalf("bound_service_account_join_keys = %#v, want one exact join key", envelope.Payload["bound_service_account_join_keys"])
	}
	for _, forbidden := range []string{"checkout", "payments-api"} {
		if joinKeys[0] == forbidden {
			t.Fatalf("bound service account join key leaked raw value %q", forbidden)
		}
	}
}

func TestVaultAuthRoleEnvelopeDoesNotEmitExactJoinForWildcardSelectors(t *testing.T) {
	t.Parallel()

	envelope, err := NewVaultAuthRoleEnvelope(VaultAuthRoleObservation{
		Context: VaultContext{
			VaultClusterID:      "vault-a",
			ScopeID:             "scope-vault",
			GenerationID:        "generation-vault",
			CollectorInstanceID: "collector-vault",
			FencingToken:        9,
			ObservedAt:          time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
			RedactionKey:        testSecretsIAMRedactionKey(),
		},
		MountPath:                     "auth/kubernetes",
		RoleName:                      "broad",
		AuthMethod:                    VaultAuthMethodKubernetes,
		KubernetesClusterID:           "cluster-a",
		BoundServiceAccountNames:      []string{"*"},
		BoundServiceAccountNamespaces: []string{"checkout"},
		TokenPolicyNames:              []string{"kv-read"},
	})
	if err != nil {
		t.Fatalf("NewVaultAuthRoleEnvelope() error = %v, want nil", err)
	}
	if got := envelope.Payload["bound_service_account_join_keys"]; got != nil {
		t.Fatalf("bound_service_account_join_keys = %#v, want nil for wildcard selector", got)
	}
	if got, want := envelope.Payload["bound_service_account_selector_wildcard"], true; got != want {
		t.Fatalf("bound_service_account_selector_wildcard = %#v, want %v", got, want)
	}
}

func TestVaultAuthRoleEnvelopeDoesNotEmitExactJoinForMixedWildcardSelectors(t *testing.T) {
	t.Parallel()

	envelope, err := NewVaultAuthRoleEnvelope(VaultAuthRoleObservation{
		Context: VaultContext{
			VaultClusterID:      "vault-a",
			ScopeID:             "scope-vault",
			GenerationID:        "generation-vault",
			CollectorInstanceID: "collector-vault",
			FencingToken:        9,
			ObservedAt:          time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
			RedactionKey:        testSecretsIAMRedactionKey(),
		},
		MountPath:                     "auth/kubernetes",
		RoleName:                      "broad",
		AuthMethod:                    VaultAuthMethodKubernetes,
		KubernetesClusterID:           "cluster-a",
		BoundServiceAccountNames:      []string{"*", "payments-api"},
		BoundServiceAccountNamespaces: []string{"checkout"},
		TokenPolicyNames:              []string{"kv-read"},
	})
	if err != nil {
		t.Fatalf("NewVaultAuthRoleEnvelope() error = %v, want nil", err)
	}
	if got := envelope.Payload["bound_service_account_join_keys"]; got != nil {
		t.Fatalf("bound_service_account_join_keys = %#v, want nil when any selector is wildcard", got)
	}
	if got, want := envelope.Payload["bound_service_account_selector_wildcard"], true; got != want {
		t.Fatalf("bound_service_account_selector_wildcard = %#v, want %v", got, want)
	}
}

func TestTrustPolicyEnvelopeEmitsSubjectFingerprintsOnly(t *testing.T) {
	t.Parallel()

	subject := "system:serviceaccount:checkout:payments-api"
	envelope, err := NewTrustPolicyEnvelope(TrustPolicyObservation{
		Context: EnvelopeContext{
			AccountID:           "123456789012",
			Region:              "us-east-1",
			ScopeID:             "scope-aws",
			GenerationID:        "generation-aws",
			CollectorInstanceID: "collector-aws",
			FencingToken:        11,
			ObservedAt:          time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
		},
		RoleARN:                        "arn:aws:iam::123456789012:role/payments-api",
		Effect:                         "Allow",
		Actions:                        []string{"sts:AssumeRoleWithWebIdentity"},
		AssumePrincipals:               []string{"arn:aws:iam::123456789012:oidc-provider/example"},
		WebIdentitySubjectFingerprints: []string{WebIdentitySubjectFingerprint(subject)},
	})
	if err != nil {
		t.Fatalf("NewTrustPolicyEnvelope() error = %v, want nil", err)
	}
	fingerprints, ok := envelope.Payload["web_identity_subject_fingerprints"].([]string)
	if !ok || len(fingerprints) != 1 || fingerprints[0] == "" {
		t.Fatalf("web_identity_subject_fingerprints = %#v, want one fingerprint", envelope.Payload["web_identity_subject_fingerprints"])
	}
	if fingerprints[0] == subject {
		t.Fatal("trust policy payload leaked raw web identity subject")
	}
	if envelope.FactKind != facts.AWSIAMTrustPolicyFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.AWSIAMTrustPolicyFactKind)
	}
}
