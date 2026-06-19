package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Demo image-identity proof constants. These mirror the exact synthetic values
// the supply-chain demo seeds in
// examples/supply-chain-demo/scripts/seed-image-identity-facts.sql and the
// committed SBOM fixture examples/supply-chain-demo/sbom/app.cdx.json. Pinning
// the test to the same literals gives a daemon-free proof that the precise
// seeded payloads join into a finding carrying image_ref, without bringing up
// the Compose stack. Nothing here is real: there is no demo.invalid registry,
// no CVE-2026-SYNTHETIC-NPM advisory, and no synthetic-vulnerable-npm package on
// any feed. See issue #3061.
const (
	demoImageIdentityCVEID         = "CVE-2026-SYNTHETIC-NPM"
	demoImageIdentityAdvisoryID    = "GHSA-synthetic-npm-0001"
	demoImageIdentityPackageID     = "npm:synthetic-vulnerable-npm"
	demoImageIdentityPURL          = "pkg:npm/synthetic-vulnerable-npm@1.0.0"
	demoImageIdentitySubjectDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	demoImageIdentityRepositoryID  = "oci-registry://demo.invalid/vuln-demo-app"
	demoImageIdentityImageRef      = "demo.invalid/vuln-demo-app@sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

// TestBuildSupplyChainImpactFindingsDemoImageIdentityHop proves the OCI
// image-identity sub-hop the live full-chain proof deliberately skips: a
// registry-observed image digest surfaces as image_ref on the impact finding.
//
// The envelopes here are the SAME reducer correlation facts that the demo's
// upstream domains (DomainContainerImageIdentity, DomainSBOMAttestationAttachment)
// publish from the seeded raw OCI manifest + SBOM document/component +
// attestation facts. This test asserts the final join performed by
// DomainSupplyChainImpact, which is what the API route the proof script polls
// returns. It is the daemon-free counterpart to
// examples/supply-chain-demo/scripts/run-image-identity-proof.sh.
//
// The match is intentionally driven by purl equality: the affected_package
// package_id (npm:synthetic-vulnerable-npm) differs from the SBOM component's
// derived packageID (pkg:npm/synthetic-vulnerable-npm), so componentMatches-
// AffectedPackage can only fire through the shared purl
// pkg:npm/synthetic-vulnerable-npm@1.0.0. If the seed ever drops the purl from
// the advisory fact, this test fails — exactly the drift the live proof would
// also catch.
func TestBuildSupplyChainImpactFindingsDemoImageIdentityHop(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		demoImageIdentityCVEFact("cve-demo"),
		demoImageIdentityAffectedPackageFact("affected-demo"),
		demoImageIdentitySBOMComponentFact("component-demo", "doc-demo"),
		sbomAttachmentImpactFact("attachment-demo", "doc-demo", demoImageIdentitySubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-demo",
			demoImageIdentitySubjectDigest,
			demoImageIdentityRepositoryID,
			demoImageIdentityImageRef,
			string(ContainerImageIdentityExactDigest),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)[demoImageIdentityCVEID]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)

	// The image-identity hop: a registry digest surfaces as image_ref. This is
	// the exact field the live proof asserts is non-empty.
	if got.ImageRef != demoImageIdentityImageRef {
		t.Fatalf("ImageRef = %q, want %q", got.ImageRef, demoImageIdentityImageRef)
	}
	if got.SubjectDigest != demoImageIdentitySubjectDigest {
		t.Fatalf("SubjectDigest = %q, want %q", got.SubjectDigest, demoImageIdentitySubjectDigest)
	}
	if got.PackageID != demoImageIdentityPackageID {
		t.Fatalf("PackageID = %q, want %q", got.PackageID, demoImageIdentityPackageID)
	}
	if got.RepositoryID != demoImageIdentityRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, demoImageIdentityRepositoryID)
	}
	if got.MatchReason != "sbom_component_path" {
		t.Fatalf("MatchReason = %q, want sbom_component_path", got.MatchReason)
	}
	if got.RuntimeReachability != "image_sbom" {
		t.Fatalf("RuntimeReachability = %q, want image_sbom", got.RuntimeReachability)
	}

	evidencePath := strings.Join(got.EvidencePath, " -> ")
	for _, want := range []string{
		facts.SBOMComponentFactKind,
		sbomAttestationAttachmentFactKind,
		containerImageIdentityFactKind,
	} {
		if !strings.Contains(evidencePath, want) {
			t.Fatalf("EvidencePath = %q, want to contain %q", evidencePath, want)
		}
	}
}

func demoImageIdentityCVEFact(factID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id":         demoImageIdentityCVEID,
			"advisory_id":    demoImageIdentityAdvisoryID,
			"source":         "osv",
			"cvss_score":     9.8,
			"severity_label": "CRITICAL",
			"aliases":        []any{demoImageIdentityCVEID, demoImageIdentityAdvisoryID},
		},
	}
}

func demoImageIdentityAffectedPackageFact(factID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":            demoImageIdentityCVEID,
			"advisory_id":       demoImageIdentityAdvisoryID,
			"source":            "osv",
			"package_id":        demoImageIdentityPackageID,
			"ecosystem":         "npm",
			"package_name":      "synthetic-vulnerable-npm",
			"purl":              demoImageIdentityPURL,
			"affected_versions": []any{"1.0.0"},
			"fixed_versions":    []any{"1.0.1"},
		},
	}
}

// demoImageIdentitySBOMComponentFact mirrors the seeded sbom.component fact for
// synthetic-vulnerable-npm@1.0.0. The version (1.0.0) matches the affected
// version so the SBOM-derived path classifies affected_derived rather than
// possibly_affected.
func demoImageIdentitySBOMComponentFact(factID string, documentID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMComponentFactKind,
		Payload: map[string]any{
			"document_id": documentID,
			"purl":        demoImageIdentityPURL,
			"name":        "synthetic-vulnerable-npm",
			"version":     "1.0.0",
		},
	}
}
