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

	deploymentImageRef := "registry.example/api@" + testImpactSubjectDigest
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

	tagImageRef := "registry.example/api:v2"
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
