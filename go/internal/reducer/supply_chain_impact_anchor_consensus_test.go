// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactFindingIdentityStableAcrossAnchorFactIDDraws is the
// #5887 finding-identity regression guard, one layer past the anchor-tier
// fix pinned in TestPreferSupplyChainImageIdentityConsensusSurvivesUnluckyFactIDDraw
// (supply_chain_impact_index_build_test.go).
//
// supplyChainImpactLogicalIdentity (supply_chain_impact_writer.go) includes
// finding.RepositoryID, and that identity map feeds BOTH
// supplyChainImpactFindingID (a facts.StableID hash) and
// supplyChainImpactStableFactKey (a literal colon-joined string with
// repository_id as one of its components). finding.RepositoryID is itself
// stamped from singleSupplyChainImageSourceRepositoryID(winner) in
// supply_chain_impact_index.go's os_package join -- the exact winner
// preferSupplyChainImageIdentityConsensus resolves.
//
// So the #5887 bug was never only a mislabeled anchor: when the anchor
// flipped between runs, the SAME underlying finding got a DIFFERENT
// FactID and a different StableFactKey between runs too, because
// RepositoryID is part of its identity. A suppression keyed to one run's
// identity would silently stop matching a later run's -- observed directly
// on #5904's golden-corpus gate failure (an "ignored" suppression state
// coming back "active" with no error, because the finding it targeted no
// longer existed under that key).
//
// This test pins the invariant at the identity-FUNCTION layer (no fixture
// harness, no scope dispatch, no suppression machinery): given two
// structurally different envelope "draws" for the same digest -- mirroring
// two different per-run generation_id hash draws -- that both correctly
// resolve to the SAME repository post-fix, a finding built from either draw
// MUST produce a byte-identical supplyChainImpactFindingID and
// supplyChainImpactStableFactKey. If a future change makes RepositoryID (or
// any other field folded into supplyChainImpactLogicalIdentity) depend on a
// per-run value again, this test -- not just the anchor-tier test -- is what
// catches it, because it asserts the property users actually feel
// (suppression/finding identity), not only the intermediate anchor value.
func TestSupplyChainImpactFindingIdentityStableAcrossAnchorFactIDDraws(t *testing.T) {
	t.Parallel()

	const (
		digest       = "sha256:5887findingid00000000000000000000000000000000000000000000000000"
		deployRepoID = "repository:r_217415d9"
		buildRepoID  = "repository:r_69256c06"
		decoy        = "oci-registry://registry.example/5887-finding-identity-app"
	)

	// drawOne puts the lone build-repo row's factID first/smallest; drawTwo
	// puts a DIFFERENT deploy-repo row's factID smallest instead. Before
	// #5887's fix, a bare factID tie-break could pick a different repository
	// winner between these two draws; after the fix, corroboration (19 rows
	// vs 1) decides both, so both draws must resolve the same repository.
	drawOne := []facts.Envelope{
		containerImageIdentityImpactFactWithSourceRepositoryIDs("0000-draw-one-ci-row", digest, decoy, buildRepoID),
	}
	drawTwo := []facts.Envelope{
		containerImageIdentityImpactFactWithSourceRepositoryIDs("zzzz-draw-two-ci-row", digest, decoy, buildRepoID),
	}
	for i := 0; i < 19; i++ {
		drawOne = append(drawOne, containerImageIdentityImpactFactWithSourceRepositoryIDs(
			fmt.Sprintf("aaaa-draw-one-deploy-%02d", i), digest, decoy, deployRepoID,
		))
		drawTwo = append(drawTwo, containerImageIdentityImpactFactWithSourceRepositoryIDs(
			fmt.Sprintf("mmmm-draw-two-deploy-%02d", i), digest, decoy, deployRepoID,
		))
	}

	repoOne := singleSupplyChainImageSourceRepositoryID(bestSupplyChainImageIdentitiesByDigest(drawOne)[digest])
	repoTwo := singleSupplyChainImageSourceRepositoryID(bestSupplyChainImageIdentitiesByDigest(drawTwo)[digest])
	if repoOne != deployRepoID || repoTwo != deployRepoID {
		t.Fatalf(
			"resolved repository differs across draws: draw one = %q, draw two = %q, want both %q",
			repoOne, repoTwo, deployRepoID,
		)
	}

	// The SAME underlying finding (identical CVE/package/digest/etc.) as
	// resolved by either draw -- only RepositoryID is draw-dependent input,
	// and it must already be equal by the assertion above.
	base := SupplyChainImpactFinding{
		CVEID:           "CVE-2026-5887",
		PackageID:       "pkg:deb/debian/openssl",
		PURL:            "pkg:deb/debian/openssl@3.0.11",
		ProductCriteria: "cpe:2.3:a:openssl:openssl:3.0.11:*:*:*:*:*:*:*",
		ObservedVersion: "3.0.11",
		RequestedRange:  "< 3.0.12",
		Status:          SupplyChainImpactAffectedExact,
		SubjectDigest:   digest,
	}
	findingOne := base
	findingOne.RepositoryID = repoOne
	findingTwo := base
	findingTwo.RepositoryID = repoTwo

	write := SupplyChainImpactWrite{ScopeID: "scope-5887-finding-identity", GenerationID: "generation-5887-finding-identity"}
	keyOne := supplyChainImpactStableFactKey(write, findingOne)
	keyTwo := supplyChainImpactStableFactKey(write, findingTwo)
	if keyOne != keyTwo {
		t.Fatalf(
			"supplyChainImpactStableFactKey differs across draws: draw one = %q, draw two = %q -- a suppression keyed to one draw's identity would silently stop matching the other (#5904)",
			keyOne, keyTwo,
		)
	}

	idOne := supplyChainImpactFindingID(findingOne)
	idTwo := supplyChainImpactFindingID(findingTwo)
	if idOne != idTwo {
		t.Fatalf("supplyChainImpactFindingID differs across draws: draw one = %q, draw two = %q", idOne, idTwo)
	}
}
