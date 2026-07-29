// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildSupplyChainImpactFindingsBakesCIDeclaredArtifactDigestOnDigestMatch
// proves the #5469 baking rule: a cicd_run_correlation deployment that matches
// the finding through the STRONG digest branch
// (supplyChainDeploymentMatchesFinding's first check) has its own declared
// digest and image ref persisted onto the finding as
// CIDeclaredArtifactDigest/CIDeclaredImageRef, so the query-time version
// resolver can disclose a real CI-declared claim instead of borrowing the
// finding's own SubjectDigest (issue #5469 review finding F1).
func TestBuildSupplyChainImpactFindingsBakesCIDeclaredArtifactDigestOnDigestMatch(t *testing.T) {
	t.Parallel()

	deploymentImageRef := "registry.example.com/api@" + testImpactSubjectDigest
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-5469", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-5469", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"api", "example"}, 2, false),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-1",
			testImpactSubjectDigest,
			testImpactRepositoryID,
			deploymentImageRef,
			string(ContainerImageIdentityExactDigest),
		),
		cicdRunCorrelationImpactFact(
			"deploy-1",
			testImpactSubjectDigest,
			deploymentImageRef,
			testImpactRepositoryID,
			testImpactEnv,
			string(CICDRunCorrelationExact),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5469"]
	if got.CIDeclaredArtifactDigest != testImpactSubjectDigest {
		t.Fatalf("CIDeclaredArtifactDigest = %q, want %q", got.CIDeclaredArtifactDigest, testImpactSubjectDigest)
	}
	if got.CIDeclaredImageRef != deploymentImageRef {
		t.Fatalf("CIDeclaredImageRef = %q, want %q", got.CIDeclaredImageRef, deploymentImageRef)
	}
}

// TestBuildSupplyChainImpactFindingsBakesContradictingDigestOnImageRefMatch
// proves baking is anchored to the STRONG branch that actually matched, not
// to the finding's own SubjectDigest: the deployment here matches only
// through the image-ref identity branch (its own artifact_digest deliberately
// contradicts the finding's SubjectDigest), so the deployment's OWN
// (contradicting) digest is what gets baked -- exactly the real-world case a
// deployment whose tag has moved to a different build represents, and the
// case #5469's version-resolution corroboration needs to disclose as a
// genuine same-axis digest disagreement instead of one that was previously
// architecturally impossible to observe (review finding F2/F3).
func TestBuildSupplyChainImpactFindingsBakesContradictingDigestOnImageRefMatch(t *testing.T) {
	t.Parallel()

	tagImageRef := "registry.example.com/api:v2"
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-5470", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-5470", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"api", "example"}, 2, false),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-1",
			testImpactSubjectDigest,
			testImpactRepositoryID,
			tagImageRef,
			string(ContainerImageIdentityExactDigest),
		),
		// The deployment's own artifact_digest (testImpactOtherDigest)
		// deliberately contradicts the finding's SubjectDigest, so the
		// digest branch cannot match -- only the image-ref equality
		// (tagImageRef == tagImageRef) can, and does.
		cicdRunCorrelationImpactFact(
			"deploy-1",
			testImpactOtherDigest,
			tagImageRef,
			testImpactRepositoryID,
			testImpactEnv,
			string(CICDRunCorrelationExact),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5470"]
	if got.RuntimeReachability == "deployed_image" {
		t.Fatalf("RuntimeReachability = deployed_image, want NOT promoted: the deployment's digest contradicts the finding's SubjectDigest")
	}
	if got.CIDeclaredArtifactDigest != testImpactOtherDigest {
		t.Fatalf("CIDeclaredArtifactDigest = %q, want the deployment's own contradicting digest %q", got.CIDeclaredArtifactDigest, testImpactOtherDigest)
	}
	if got.CIDeclaredImageRef != tagImageRef {
		t.Fatalf("CIDeclaredImageRef = %q, want %q", got.CIDeclaredImageRef, tagImageRef)
	}
}

// TestBuildSupplyChainImpactFindingsPrefersExactDigestAcrossFactPermutations
// proves immutable subject-digest evidence outranks a mutable image-reference
// match regardless of fact order. The selected digest and image reference must
// still come from one deployment.
func TestBuildSupplyChainImpactFindingsPrefersExactDigestAcrossFactPermutations(t *testing.T) {
	t.Parallel()

	findingImageRef := "registry.example.com/api@" + testImpactSubjectDigest
	imageRefOnly := cicdRunCorrelationImpactFact(
		"deploy-image-ref",
		testImpactOtherDigest,
		findingImageRef,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
	)
	imageRefOnlyWithoutDigest := cicdRunCorrelationImpactFact(
		"deploy-image-ref-no-digest",
		"",
		findingImageRef,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
	)
	exactDigest := cicdRunCorrelationImpactFact(
		"deploy-digest",
		testImpactSubjectDigest,
		testImpactOtherImageRef,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
	)

	tests := []struct {
		name        string
		deployments []facts.Envelope
	}{
		{
			name:        "conflicting mutable image reference first",
			deployments: []facts.Envelope{imageRefOnly, exactDigest},
		},
		{
			name:        "exact digest before conflicting mutable image reference",
			deployments: []facts.Envelope{exactDigest, imageRefOnly},
		},
		{
			name:        "digestless mutable image reference first",
			deployments: []facts.Envelope{imageRefOnlyWithoutDigest, exactDigest},
		},
		{
			name:        "exact digest before digestless mutable image reference",
			deployments: []facts.Envelope{exactDigest, imageRefOnlyWithoutDigest},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := buildSupplyChainImpactFindingWithDeployments(
				"CVE-2026-5472",
				findingImageRef,
				test.deployments,
			)
			if got.CIDeclaredArtifactDigest != testImpactSubjectDigest {
				t.Fatalf("CIDeclaredArtifactDigest = %q, want exact subject digest %q", got.CIDeclaredArtifactDigest, testImpactSubjectDigest)
			}
			if got.CIDeclaredImageRef != testImpactOtherImageRef {
				t.Fatalf("CIDeclaredImageRef = %q, want exact-digest deployment ref %q", got.CIDeclaredImageRef, testImpactOtherImageRef)
			}
		})
	}
}

// TestBuildSupplyChainImpactFindingsUsesFactOrderWithinEqualMatchStrength
// proves fact order is the deterministic tie-break only after match strength.
func TestBuildSupplyChainImpactFindingsUsesFactOrderWithinEqualMatchStrength(t *testing.T) {
	t.Parallel()

	findingImageRef := "registry.example.com/api@" + testImpactSubjectDigest
	thirdDigest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	tests := []struct {
		name        string
		deployments []facts.Envelope
		wantDigest  string
		wantImage   string
	}{
		{
			name: "first exact digest",
			deployments: []facts.Envelope{
				cicdRunCorrelationImpactFact("deploy-a", testImpactSubjectDigest, "registry.example.com/api:digest-a", testImpactRepositoryID, testImpactEnv, string(CICDRunCorrelationExact)),
				cicdRunCorrelationImpactFact("deploy-b", testImpactSubjectDigest, "registry.example.com/api:digest-b", testImpactRepositoryID, testImpactEnv, string(CICDRunCorrelationExact)),
			},
			wantDigest: testImpactSubjectDigest,
			wantImage:  "registry.example.com/api:digest-a",
		},
		{
			name: "permuted exact digest",
			deployments: []facts.Envelope{
				cicdRunCorrelationImpactFact("deploy-b", testImpactSubjectDigest, "registry.example.com/api:digest-b", testImpactRepositoryID, testImpactEnv, string(CICDRunCorrelationExact)),
				cicdRunCorrelationImpactFact("deploy-a", testImpactSubjectDigest, "registry.example.com/api:digest-a", testImpactRepositoryID, testImpactEnv, string(CICDRunCorrelationExact)),
			},
			wantDigest: testImpactSubjectDigest,
			wantImage:  "registry.example.com/api:digest-b",
		},
		{
			name: "first image reference",
			deployments: []facts.Envelope{
				cicdRunCorrelationImpactFact("deploy-c", testImpactOtherDigest, findingImageRef, testImpactRepositoryID, testImpactEnv, string(CICDRunCorrelationExact)),
				cicdRunCorrelationImpactFact("deploy-d", thirdDigest, findingImageRef, testImpactRepositoryID, testImpactEnv, string(CICDRunCorrelationExact)),
			},
			wantDigest: testImpactOtherDigest,
			wantImage:  findingImageRef,
		},
		{
			name: "permuted image reference",
			deployments: []facts.Envelope{
				cicdRunCorrelationImpactFact("deploy-d", thirdDigest, findingImageRef, testImpactRepositoryID, testImpactEnv, string(CICDRunCorrelationExact)),
				cicdRunCorrelationImpactFact("deploy-c", testImpactOtherDigest, findingImageRef, testImpactRepositoryID, testImpactEnv, string(CICDRunCorrelationExact)),
			},
			wantDigest: thirdDigest,
			wantImage:  findingImageRef,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := buildSupplyChainImpactFindingWithDeployments(
				"CVE-2026-5473",
				findingImageRef,
				test.deployments,
			)
			if got.CIDeclaredArtifactDigest != test.wantDigest {
				t.Fatalf("CIDeclaredArtifactDigest = %q, want %q", got.CIDeclaredArtifactDigest, test.wantDigest)
			}
			if got.CIDeclaredImageRef != test.wantImage {
				t.Fatalf("CIDeclaredImageRef = %q, want %q", got.CIDeclaredImageRef, test.wantImage)
			}
		})
	}
}

func buildSupplyChainImpactFindingWithDeployments(
	cveID string,
	findingImageRef string,
	deployments []facts.Envelope,
) SupplyChainImpactFinding {
	input := []facts.Envelope{
		vulnerabilityCVEFact("cve-1", cveID, 9.1),
		vulnerabilityAffectedPackageFact("affected-1", cveID, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"api", "example"}, 2, false),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-1",
			testImpactSubjectDigest,
			testImpactRepositoryID,
			findingImageRef,
			string(ContainerImageIdentityExactDigest),
		),
	}
	input = append(input, deployments...)
	return supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(input))[cveID]
}

// TestBuildSupplyChainImpactFindingsWeakBranchBakesNoCIDeclaredArtifactIdentity
// proves the fail-closed half of the #5469 baking rule: a deployment that
// matches ONLY through the weak repository+environment+operational-anchor
// branch (#5426's branch 3, supplyChainDeploymentMatchesFinding) makes no
// artifact-identity claim at all, so it must bake neither
// CIDeclaredArtifactDigest nor CIDeclaredImageRef -- even though the
// deployment fact itself carries a (contradicting, unrelated) digest and
// image ref. Baking those would let the weak branch fabricate a CI-declared
// version/digest claim the evidence never actually made (review finding F1's
// root cause).
func TestBuildSupplyChainImpactFindingsWeakBranchBakesNoCIDeclaredArtifactIdentity(t *testing.T) {
	t.Parallel()

	deployment := cicdRunCorrelationImpactFact(
		"deploy-1",
		testImpactOtherDigest,
		testImpactOtherImageRef,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
	)
	findings := BuildSupplyChainImpactFindings(branch3TestFindingFacts("CVE-2026-5471", deployment))

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5471"]
	if got.CIDeclaredArtifactDigest != "" {
		t.Fatalf("CIDeclaredArtifactDigest = %q, want empty for a weak-branch-only match", got.CIDeclaredArtifactDigest)
	}
	if got.CIDeclaredImageRef != "" {
		t.Fatalf("CIDeclaredImageRef = %q, want empty for a weak-branch-only match", got.CIDeclaredImageRef)
	}
}
