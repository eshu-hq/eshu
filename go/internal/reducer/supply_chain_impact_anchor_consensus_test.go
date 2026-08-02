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

// TestBuildSupplyChainImageIdentityConsensusCollapsesLegacyAndV2Duplicate is
// a #5887 follow-up regression guard (found in review by codex on PR #5908).
//
// WriteContainerImageIdentityDecisions' doc comment
// (container_image_identity_writer.go) explains that during the #5854 v2
// cutover, a completeness warning can deliberately hold a legacy
// outcome-keyed row live in the same pass that publishes its stronger v2
// replacement for the IDENTICAL (scope_id, generation_id, image_ref)
// decision. Both rows land in the same envelope batch with two DIFFERENT
// fact IDs. Before this test's fix, buildSupplyChainImageIdentityConsensus
// counted one vote per envelope, so that ONE decision cast TWO votes for its
// repository while the legacy row was held -- and the count silently
// dropped back to one, potentially flipping the winning repository, the
// instant a later pass retired the legacy row. That is the SAME class of bug
// #5887 fixed (the anchor moving with no change in underlying evidence),
// reached through the cutover boundary instead of a per-run factID draw.
//
// This test constructs exactly that shape: repoLegacyV2Dup is asserted by
// TWO envelopes sharing one logical key (same ScopeID, GenerationID, and
// image_ref; different FactID; one tagged as the v2 format, one left
// untagged to represent the held legacy row), competing against a single
// row for repoSingleWriter. repoLegacyV2Dup's ID is chosen to sort AFTER
// repoSingleWriter's, so a real (not merely double-counted-vs-tied) 2-vs-1
// win is on the line: before the fix, repoLegacyV2Dup wins while both rows
// are live (2 votes beats 1), then the winner FLIPS to repoSingleWriter the
// moment the legacy row is removed and the count drops to a 1-1 tie broken
// by repository ID ordering. After the fix, both physical rows collapse to
// ONE logical vote for repoLegacyV2Dup, so removing the legacy row changes
// nothing: repoSingleWriter wins in both scenarios.
func TestBuildSupplyChainImageIdentityConsensusCollapsesLegacyAndV2Duplicate(t *testing.T) {
	t.Parallel()

	const (
		digest             = "sha256:5887legacyv2dup00000000000000000000000000000000000000000000000"
		repoLegacyV2Dup    = "repository:r_zzz_legacy_v2_dup"
		repoSingleWriter   = "repository:r_aaa_single_writer"
		sharedScopeID      = "repository:5887-legacy-v2-dup-scope"
		sharedGenerationID = "generation-5887-legacy-v2-dup"
		sharedImageRef     = "registry.example/5887-legacy-v2-dup-app:prod"
		legacyRowFactID    = "identity-5887-legacy-row"
		v2RowFactID        = "identity-5887-v2-row"
		singleWriterFactID = "identity-5887-single-writer-row"
		decoyRepositoryID  = "oci-registry://registry.example/5887-legacy-v2-dup-app"
	)

	// The v2 row: current format, tagged identity_format=image_ref_v2.
	v2Row := facts.Envelope{
		FactID:       v2RowFactID,
		FactKind:     containerImageIdentityFactKind,
		ScopeID:      sharedScopeID,
		GenerationID: sharedGenerationID,
		Payload: map[string]any{
			"digest":                digest,
			"repository_id":         decoyRepositoryID,
			"image_ref":             sharedImageRef,
			"identity_format":       containerImageIdentityFormatImageRef,
			"source_repository_ids": []string{repoLegacyV2Dup},
			"canonical_writes":      1,
		},
	}
	// The legacy row: SAME logical key (scope/generation/image_ref) and the
	// same asserted repository, but a DIFFERENT fact ID and no
	// identity_format marker -- a held-over pre-#5854 row.
	legacyRow := facts.Envelope{
		FactID:       legacyRowFactID,
		FactKind:     containerImageIdentityFactKind,
		ScopeID:      sharedScopeID,
		GenerationID: sharedGenerationID,
		Payload: map[string]any{
			"digest":                digest,
			"repository_id":         decoyRepositoryID,
			"image_ref":             sharedImageRef,
			"source_repository_ids": []string{repoLegacyV2Dup},
			"canonical_writes":      1,
		},
	}
	singleWriterRow := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		singleWriterFactID, digest, decoyRepositoryID, repoSingleWriter,
	)

	withLegacyHeld := []facts.Envelope{legacyRow, v2Row, singleWriterRow}
	legacyCleanedUp := []facts.Envelope{v2Row, singleWriterRow}

	winnerWithLegacyHeld := singleSupplyChainImageSourceRepositoryID(
		bestSupplyChainImageIdentitiesByDigest(withLegacyHeld)[digest],
	)
	winnerAfterCleanup := singleSupplyChainImageSourceRepositoryID(
		bestSupplyChainImageIdentitiesByDigest(legacyCleanedUp)[digest],
	)

	if winnerWithLegacyHeld != winnerAfterCleanup {
		t.Fatalf(
			"anchor moved when the legacy row was removed: with legacy held = %q, after cleanup = %q -- "+
				"a legacy/v2 duplicate pair must cast one vote, not two, or cleanup silently flips the anchor",
			winnerWithLegacyHeld, winnerAfterCleanup,
		)
	}
	if winnerAfterCleanup != repoSingleWriter {
		t.Fatalf(
			"winner = %q, want %q: with the duplicate correctly counted as one vote, the single-writer "+
				"repository's smaller repository ID should win the resulting 1-1 tie",
			winnerAfterCleanup, repoSingleWriter,
		)
	}
}
