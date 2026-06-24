// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"strings"
	"testing"
)

func TestAllowlistPolicyAcceptsAllowedComponent(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}

	result := policy.Verify(validManifest())
	if !result.Allowed {
		t.Fatalf("Verify().Allowed = false, want true; reason = %q", result.Reason)
	}
}

func TestDisabledPolicyRejectsComponent(t *testing.T) {
	t.Parallel()

	result := Policy{Mode: TrustModeDisabled}.Verify(validManifest())
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if !strings.Contains(result.Reason, "disabled") {
		t.Fatalf("Verify().Reason = %q, want disabled reason", result.Reason)
	}
}

func TestPolicyRejectsRevokedPublisher(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		RevokedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}

	result := policy.Verify(validManifest())
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if !strings.Contains(result.Reason, "revoked publisher") {
		t.Fatalf("Verify().Reason = %q, want revoked publisher reason", result.Reason)
	}
}

func TestPolicyRejectsRevokedPublisherBeforeIncompatibleCore(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.CompatibleCore = ">=0.1.0 <0.2.0"
	policy := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		RevokedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}

	result := policy.Verify(manifest)
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if result.Code != ErrorCodeRevokedPackage {
		t.Fatalf("Verify().Code = %q, want %q", result.Code, ErrorCodeRevokedPackage)
	}
	if !strings.Contains(result.Reason, "revoked publisher") {
		t.Fatalf("Verify().Reason = %q, want revoked publisher reason", result.Reason)
	}
}

func TestPolicyRejectsRevokedComponentBeforeIncompatibleCore(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.CompatibleCore = ">=0.1.0 <0.2.0"
	policy := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		RevokedIDs:        []string{"dev.eshu.collector.aws"},
		CoreVersion:       "v0.0.5",
	}

	result := policy.Verify(manifest)
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if result.Code != ErrorCodeRevokedPackage {
		t.Fatalf("Verify().Code = %q, want %q", result.Code, ErrorCodeRevokedPackage)
	}
	if !strings.Contains(result.Reason, "revoked") {
		t.Fatalf("Verify().Reason = %q, want revoked component reason", result.Reason)
	}
}

func TestPolicyRejectsInvalidCompatibleCoreBeforeRevocation(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.CompatibleCore = ">=0.1.0 nope"
	policy := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		RevokedIDs:        []string{"dev.eshu.collector.aws"},
		CoreVersion:       "v0.0.5",
	}

	result := policy.Verify(manifest)
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if result.Code != ErrorCodeInvalidManifest {
		t.Fatalf("Verify().Code = %q, want %q", result.Code, ErrorCodeInvalidManifest)
	}
}

func TestStrictPolicyFailsClosedWithoutProvenanceVerifier(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Mode:              TrustModeStrict,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}

	result := policy.Verify(validManifest())
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if !strings.Contains(result.Reason, "provenance verification") {
		t.Fatalf("Verify().Reason = %q, want provenance verification reason", result.Reason)
	}
}

func TestStrictPolicyAcceptsVerifiedProvenance(t *testing.T) {
	t.Parallel()

	verifier := &recordingProvenanceVerifier{}
	policy := Policy{
		Mode:              TrustModeStrict,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
		Provenance: ProvenancePolicy{
			CertificateIdentity: "https://github.com/eshu-hq/eshu/.github/workflows/release.yml@refs/tags/v0.1.0",
			OIDCIssuer:          "https://token.actions.githubusercontent.com",
		},
		ProvenanceVerifier: verifier,
	}

	result := policy.Verify(validManifest())
	if !result.Allowed {
		t.Fatalf("Verify().Allowed = false, want true; reason = %q", result.Reason)
	}
	if verifier.calls != 1 {
		t.Fatalf("verifier calls = %d, want 1", verifier.calls)
	}
	if verifier.requirement.CertificateIdentity != policy.Provenance.CertificateIdentity {
		t.Fatalf("verifier identity = %q, want %q", verifier.requirement.CertificateIdentity, policy.Provenance.CertificateIdentity)
	}
	if verifier.requirement.PredicateType != DefaultProvenancePredicateType {
		t.Fatalf("verifier predicate type = %q, want default %q", verifier.requirement.PredicateType, DefaultProvenancePredicateType)
	}
}

func TestStrictPolicyRejectsMissingProvenanceIdentity(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Mode:              TrustModeStrict,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
		Provenance: ProvenancePolicy{
			OIDCIssuer: "https://token.actions.githubusercontent.com",
		},
		ProvenanceVerifier: &recordingProvenanceVerifier{},
	}

	result := policy.Verify(validManifest())
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if result.Code != ErrorCodeProvenanceRequired {
		t.Fatalf("Verify().Code = %q, want %q", result.Code, ErrorCodeProvenanceRequired)
	}
	if !strings.Contains(result.Reason, "certificate identity") {
		t.Fatalf("Verify().Reason = %q, want certificate identity reason", result.Reason)
	}
}

func TestStrictPolicyRejectsUnallowlistedPublisherBeforeProvenance(t *testing.T) {
	t.Parallel()

	verifier := &recordingProvenanceVerifier{}
	policy := Policy{
		Mode:              TrustModeStrict,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"other-publisher"},
		CoreVersion:       "v0.0.5",
		Provenance: ProvenancePolicy{
			CertificateIdentity: "https://github.com/eshu-hq/eshu/.github/workflows/release.yml@refs/tags/v0.1.0",
			OIDCIssuer:          "https://token.actions.githubusercontent.com",
		},
		ProvenanceVerifier: verifier,
	}

	result := policy.Verify(validManifest())
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if verifier.calls != 0 {
		t.Fatalf("verifier calls = %d, want 0", verifier.calls)
	}
	if !strings.Contains(result.Reason, "publisher") {
		t.Fatalf("Verify().Reason = %q, want publisher allowlist reason", result.Reason)
	}
}

func TestStrictPolicyRejectsVerifierFailureReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		code ErrorCode
	}{
		{
			name: "missing signature",
			err:  NewError(ErrorCodeProvenanceInvalid, "missing cosign signature for artifact 0"),
			code: ErrorCodeProvenanceInvalid,
		},
		{
			name: "wrong digest",
			err:  NewError(ErrorCodeProvenanceInvalid, "cosign claims did not match artifact digest for artifact 0"),
			code: ErrorCodeProvenanceInvalid,
		},
		{
			name: "wrong publisher identity",
			err:  NewError(ErrorCodeUntrustedPublisher, "provenance identity is not trusted for publisher eshu-hq"),
			code: ErrorCodeUntrustedPublisher,
		},
		{
			name: "unsupported provenance shape",
			err:  NewError(ErrorCodeUnsupportedProvenance, "unsupported attestation predicate for artifact 0"),
			code: ErrorCodeUnsupportedProvenance,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			policy := Policy{
				Mode:              TrustModeStrict,
				AllowedIDs:        []string{"dev.eshu.collector.aws"},
				AllowedPublishers: []string{"eshu-hq"},
				CoreVersion:       "v0.0.5",
				Provenance: ProvenancePolicy{
					CertificateIdentity: "https://github.com/eshu-hq/eshu/.github/workflows/release.yml@refs/tags/v0.1.0",
					OIDCIssuer:          "https://token.actions.githubusercontent.com",
				},
				ProvenanceVerifier: &recordingProvenanceVerifier{err: tc.err},
			}

			result := policy.Verify(validManifest())
			if result.Allowed {
				t.Fatal("Verify().Allowed = true, want false")
			}
			if result.Code != tc.code {
				t.Fatalf("Verify().Code = %q, want %q", result.Code, tc.code)
			}
			if result.Reason == "" {
				t.Fatal("Verify().Reason is empty, want verifier failure reason")
			}
		})
	}
}

func TestConfigureProvenanceFromEnvAddsCosignVerifierOnlyForStrictMode(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		values := map[string]string{
			"ESHU_COMPONENT_PROVENANCE_CERTIFICATE_IDENTITY": "https://github.com/eshu-hq/eshu/.github/workflows/release.yml@refs/tags/v0.1.0",
			"ESHU_COMPONENT_PROVENANCE_OIDC_ISSUER":          "https://token.actions.githubusercontent.com",
			"ESHU_COMPONENT_PROVENANCE_PREDICATE_TYPE":       DefaultProvenancePredicateType,
			"ESHU_COMPONENT_COSIGN_BINARY":                   "/usr/local/bin/cosign",
		}
		return values[key]
	}

	allowlist := ConfigureProvenanceFromEnv(Policy{Mode: TrustModeAllowlist}, getenv)
	if allowlist.ProvenanceVerifier != nil {
		t.Fatalf("allowlist ProvenanceVerifier = %#v, want nil", allowlist.ProvenanceVerifier)
	}
	if allowlist.Provenance.CertificateIdentity == "" {
		t.Fatal("allowlist Provenance.CertificateIdentity is empty, want env value preserved")
	}

	strict := ConfigureProvenanceFromEnv(Policy{Mode: TrustModeStrict}, getenv)
	verifier, ok := strict.ProvenanceVerifier.(CosignProvenanceVerifier)
	if !ok {
		t.Fatalf("strict ProvenanceVerifier = %T, want CosignProvenanceVerifier", strict.ProvenanceVerifier)
	}
	if verifier.Command != "/usr/local/bin/cosign" {
		t.Fatalf("strict verifier command = %q, want env command", verifier.Command)
	}
	if strict.Provenance.OIDCIssuer != "https://token.actions.githubusercontent.com" {
		t.Fatalf("strict Provenance.OIDCIssuer = %q, want env issuer", strict.Provenance.OIDCIssuer)
	}
}

func TestPolicyRejectsInvalidCompatibleCoreRange(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.CompatibleCore = ">=0.0.5 nope"
	policy := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}

	result := policy.Verify(manifest)
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if !strings.Contains(result.Reason, "compatibleCore") {
		t.Fatalf("Verify().Reason = %q, want compatibleCore reason", result.Reason)
	}
}

func TestPolicyRejectsIncompatibleCoreVersion(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.CompatibleCore = ">=0.1.0 <0.2.0"
	policy := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}

	result := policy.Verify(manifest)
	if result.Allowed {
		t.Fatal("Verify().Allowed = true, want false")
	}
	if !strings.Contains(result.Reason, "incompatible") {
		t.Fatalf("Verify().Reason = %q, want incompatible reason", result.Reason)
	}
}

type recordingProvenanceVerifier struct {
	calls       int
	requirement ProvenanceRequirement
	err         error
}

func (v *recordingProvenanceVerifier) VerifyProvenance(
	_ context.Context,
	_ Manifest,
	requirement ProvenanceRequirement,
) error {
	v.calls++
	v.requirement = requirement
	return v.err
}
