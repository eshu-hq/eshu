package component

import (
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
