// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactIndexContainerImageIdentityDeterministicAcrossEnvelopeOrder
// is the determinism regression guard for #5464 layer 2: the producer writes
// one reducer_container_image_identity row per triggering scope/ref with no
// per-digest canonicalization, so a single digest can carry many rows that
// disagree on source_repository_ids (this repo's own live corpus has 16 rows
// for one digest, fifteen naming one repository and one naming two). Both
// bestSupplyChainImageIdentitiesByDigest (the batch form) and the incremental
// addSupplyChainImpactIndexEntry case buildSupplyChainImpactIndexWithQuarantine
// drives one envelope at a time MUST reach the SAME winner regardless of
// which order the envelopes arrive in -- an unconditional last-write-wins
// assignment would make the winner (and therefore whether RepositoryID
// resolves at all) an accident of envelope iteration order.
func TestSupplyChainImpactIndexContainerImageIdentityDeterministicAcrossEnvelopeOrder(t *testing.T) {
	t.Parallel()

	const (
		digest              = "sha256:deterministic00000000000000000000000000000000000000000000000"
		decoyRepositoryID   = "oci-registry://registry.example/deterministic-app"
		wantRepositoryID    = "repository:r_deterministic"
		otherRepositoryID   = "repository:r_deterministic_other"
		wantWinningFactID   = "identity-deterministic-a"
		losingUnambiguousID = "identity-deterministic-b"
		ambiguousRowsFactID = "identity-deterministic-c"
	)

	// unambiguousA and unambiguousB both name exactly one (the SAME) source
	// repository -- mirroring the corpus's fifteen agreeing rows -- and
	// wantWinningFactID ("identity-deterministic-a") is the lexicographically
	// smaller of the two, so the tie-break is exercised, not just the
	// ambiguous-vs-unambiguous preference.
	unambiguousA := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		wantWinningFactID, digest, decoyRepositoryID, wantRepositoryID,
	)
	unambiguousB := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		losingUnambiguousID, digest, decoyRepositoryID, wantRepositoryID,
	)
	// ambiguous mirrors the corpus's one disagreeing row: two distinct source
	// repositories, so singleSupplyChainImageSourceRepositoryID returns "" for
	// it and it must never win over either unambiguous row.
	ambiguous := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		ambiguousRowsFactID, digest, decoyRepositoryID, wantRepositoryID, otherRepositoryID,
	)

	forward := []facts.Envelope{ambiguous, unambiguousB, unambiguousA}
	reverse := []facts.Envelope{unambiguousA, unambiguousB, ambiguous}

	forwardWinners := bestSupplyChainImageIdentitiesByDigest(forward)
	reverseWinners := bestSupplyChainImageIdentitiesByDigest(reverse)

	forwardWinner := forwardWinners[digest]
	reverseWinner := reverseWinners[digest]

	if got := singleSupplyChainImageSourceRepositoryID(forwardWinner); got != wantRepositoryID {
		t.Fatalf("forward-order winner repository = %q, want %q", got, wantRepositoryID)
	}
	if got := singleSupplyChainImageSourceRepositoryID(reverseWinner); got != wantRepositoryID {
		t.Fatalf("reverse-order winner repository = %q, want %q: winner depends on envelope order", got, wantRepositoryID)
	}
	if forwardWinner.factID != wantWinningFactID {
		t.Fatalf("forward-order winner factID = %q, want the lexicographically smaller unambiguous row %q", forwardWinner.factID, wantWinningFactID)
	}
	if reverseWinner.factID != wantWinningFactID {
		t.Fatalf("reverse-order winner factID = %q, want %q: tie-break must not depend on envelope order", reverseWinner.factID, wantWinningFactID)
	}

	// The incremental per-envelope index-build path (what the real reducer
	// intent path drives) must reach the identical winner as the batch helper.
	forwardIndex, _, err := buildSupplyChainImpactIndexWithQuarantine(forward)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactIndexWithQuarantine(forward) error = %v", err)
	}
	reverseIndex, _, err := buildSupplyChainImpactIndexWithQuarantine(reverse)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactIndexWithQuarantine(reverse) error = %v", err)
	}
	if got := singleSupplyChainImageSourceRepositoryID(forwardIndex.images[digest]); got != wantRepositoryID {
		t.Fatalf("forward index.images[digest] repository = %q, want %q", got, wantRepositoryID)
	}
	if got := singleSupplyChainImageSourceRepositoryID(reverseIndex.images[digest]); got != wantRepositoryID {
		t.Fatalf("reverse index.images[digest] repository = %q, want %q: index build depends on envelope order", got, wantRepositoryID)
	}
}

// TestSupplyChainImpactIndexContainerImageIdentityDeterministicAcrossEnvelopeOrderWithBuildProvenanceRow
// extends the determinism guard above to a tier B row (resolvable only via
// buildProvenanceRepositoryIDs, the tier this PR introduces): the prior
// determinism test above only mixes tier A (unambiguous by
// sourceRepositoryIDs) and tier C (fully ambiguous) rows, never a tier B row,
// so it could not catch an order-dependence bug specific to tier B's
// placement in supplyChainImageIdentityAnchorTier/preferSupplyChainImageIdentity.
//
// Order-independence must hold across ALL three tiers, not just tier A vs
// tier C: preferSupplyChainImageIdentity folds envelopes one pair at a time
// via bestSupplyChainImageIdentitiesByDigest/addSupplyChainImpactIndexEntry,
// so the winner after N folds must be identical regardless of which two rows
// happen to be compared first. If tier B's comparison were accidentally
// order-sensitive (e.g. by comparing against a stale "existing" value instead
// of re-deriving the tier for both candidates every time), the winner could
// flip between forward and reverse envelope order exactly like the pre-#5813
// regression did for tier A vs tier C -- this test would only catch that class
// of bug for tier B if a tier B row actually participates.
func TestSupplyChainImpactIndexContainerImageIdentityDeterministicAcrossEnvelopeOrderWithBuildProvenanceRow(t *testing.T) {
	t.Parallel()

	const (
		digest            = "sha256:deterministicb0000000000000000000000000000000000000000000000"
		decoyRepositoryID = "oci-registry://registry.example/deterministic-tierb-app"
		buildRepoID       = "repository:r_deterministic_tierb_builder"
		otherRepoID       = "repository:r_deterministic_tierb_other"
		wantWinningFactID = "identity-deterministic-tierb-a"
		losingTierBFactID = "identity-deterministic-tierb-b"
		tierCLosingFactID = "identity-deterministic-tierb-c"
	)

	// Two tier B rows: each is ambiguous by sourceRepositoryIDs alone (names
	// both the builder and an unrelated repository) but resolves unambiguously
	// via buildProvenanceRepositoryIDs to the SAME builder -- mirroring how the
	// corpus's tier A rows agree, but at tier B. wantWinningFactID is the
	// lexicographically smaller of the two, so the same-tier tie-break is
	// exercised alongside the tier check itself.
	tierBRowA := containerImageIdentityImpactFactWithBuildProvenance(
		wantWinningFactID, digest, decoyRepositoryID, buildRepoID, buildRepoID, otherRepoID,
	)
	tierBRowB := containerImageIdentityImpactFactWithBuildProvenance(
		losingTierBFactID, digest, decoyRepositoryID, buildRepoID, buildRepoID, otherRepoID,
	)
	// A tier C row (ambiguous by sourceRepositoryIDs, no build provenance at
	// all) that must never win over either tier B row, in either order.
	tierCRow := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		tierCLosingFactID, digest, decoyRepositoryID, buildRepoID, otherRepoID,
	)

	forward := []facts.Envelope{tierCRow, tierBRowB, tierBRowA}
	reverse := []facts.Envelope{tierBRowA, tierBRowB, tierCRow}

	forwardWinners := bestSupplyChainImageIdentitiesByDigest(forward)
	reverseWinners := bestSupplyChainImageIdentitiesByDigest(reverse)

	forwardWinner := forwardWinners[digest]
	reverseWinner := reverseWinners[digest]

	if got := singleSupplyChainImageSourceRepositoryID(forwardWinner); got != buildRepoID {
		t.Fatalf("forward-order winner repository = %q, want %q", got, buildRepoID)
	}
	if got := singleSupplyChainImageSourceRepositoryID(reverseWinner); got != buildRepoID {
		t.Fatalf("reverse-order winner repository = %q, want %q: winner depends on envelope order", got, buildRepoID)
	}
	if forwardWinner.factID != wantWinningFactID {
		t.Fatalf("forward-order winner factID = %q, want the lexicographically smaller tier B row %q", forwardWinner.factID, wantWinningFactID)
	}
	if reverseWinner.factID != wantWinningFactID {
		t.Fatalf("reverse-order winner factID = %q, want %q: tie-break must not depend on envelope order", reverseWinner.factID, wantWinningFactID)
	}

	forwardIndex, _, err := buildSupplyChainImpactIndexWithQuarantine(forward)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactIndexWithQuarantine(forward) error = %v", err)
	}
	reverseIndex, _, err := buildSupplyChainImpactIndexWithQuarantine(reverse)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactIndexWithQuarantine(reverse) error = %v", err)
	}
	if got := singleSupplyChainImageSourceRepositoryID(forwardIndex.images[digest]); got != buildRepoID {
		t.Fatalf("forward index.images[digest] repository = %q, want %q", got, buildRepoID)
	}
	if got := singleSupplyChainImageSourceRepositoryID(reverseIndex.images[digest]); got != buildRepoID {
		t.Fatalf("reverse index.images[digest] repository = %q, want %q: index build depends on envelope order", got, buildRepoID)
	}
}

// TestPreferSupplyChainImageIdentityUnambiguousBeatsAmbiguous is the direct
// unit guard on preferSupplyChainImageIdentity's preference rule, independent
// of the batch/index-build callers above.
func TestPreferSupplyChainImageIdentityUnambiguousBeatsAmbiguous(t *testing.T) {
	t.Parallel()

	unambiguous := supplyChainImageIdentity{factID: "z-unambiguous", sourceRepositoryIDs: []string{"repository:r_only"}}
	ambiguous := supplyChainImageIdentity{factID: "a-ambiguous", sourceRepositoryIDs: []string{"repository:r_one", "repository:r_two"}}

	// Ambiguous factID sorts first lexicographically, proving the preference
	// (unambiguous wins) overrides the tie-break rather than the reverse.
	if got := preferSupplyChainImageIdentity(ambiguous, unambiguous); got.factID != unambiguous.factID {
		t.Fatalf("preferSupplyChainImageIdentity(ambiguous, unambiguous) = %q, want the unambiguous row %q", got.factID, unambiguous.factID)
	}
	if got := preferSupplyChainImageIdentity(unambiguous, ambiguous); got.factID != unambiguous.factID {
		t.Fatalf("preferSupplyChainImageIdentity(unambiguous, ambiguous) = %q, want the unambiguous row %q", got.factID, unambiguous.factID)
	}
}

// TestPreferSupplyChainImageIdentitySourceConsensusBeatsBuildProvenanceRow is
// the #5813 golden-corpus regression guard: it reproduces the exact 20-repo
// corpus shape for digest sha256:abcdef...ab -- fifteen rows agreeing on the
// DEPLOYING repository via sourceRepositoryIDs alone, and one row that names
// BOTH the deploying repository and its own building repository in
// sourceRepositoryIDs (making sourceRepositoryIDs ambiguous for that row) but
// resolves unambiguously via buildProvenanceRepositoryIDs.
//
// Before #5813's fix, preferSupplyChainImageIdentity's single "unambiguous"
// boolean treated singleSupplyChainImageSourceRepositoryID's row-level
// provenance-first result as the ENTIRE preference signal, so the
// two-repository row (rendered "unambiguous" only via its own build
// evidence) competed on equal footing with -- and won over -- the fifteen
// genuinely-agreeing rows whenever its factID happened to sort smaller. That
// is wrong: a row resolvable only via its own build provenance must never
// outrank rows where the broader source-repository set itself already
// agrees. This test pins the fixed three-tier rule (tier A: source
// consensus > tier B: build-provenance-only > tier C: unresolved) using the
// SMALLEST factID on the two-repository row, so a regression back to the old
// single-boolean preference would make this test fail exactly as the golden
// corpus did (winner resolves the builder, not the deployer).
func TestPreferSupplyChainImageIdentitySourceConsensusBeatsBuildProvenanceRow(t *testing.T) {
	t.Parallel()

	const (
		digest       = "sha256:abcdef00000000000000000000000000000000000000000000000000000ab"
		deployRepoID = "repository:r_217415d9"
		buildRepoID  = "repository:r_69256c06"
	)

	agreeingRowOne := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"identity-5813-agree-1", digest, "oci-registry://registry.example/consensus-app", deployRepoID,
	)
	agreeingRowTwo := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"identity-5813-agree-2", digest, "oci-registry://registry.example/consensus-app", deployRepoID,
	)
	// The one ambiguous-by-source row: names BOTH the builder and the
	// deployer in sourceRepositoryIDs (so singleSupplyChainRepositoryID
	// alone returns "" for it), but buildProvenanceRepositoryIDs carries
	// only the builder, unambiguously. Its factID ("identity-5813-0-build")
	// is DELIBERATELY the lexicographically smallest of all three rows, so
	// a passing test proves the tier rule -- not the tie-break -- decided
	// the winner.
	ambiguousSourceButBuildResolvable := containerImageIdentityImpactFactWithBuildProvenance(
		"identity-5813-0-build", digest, "oci-registry://registry.example/consensus-app",
		buildRepoID, buildRepoID, deployRepoID,
	)

	envelopes := []facts.Envelope{agreeingRowOne, ambiguousSourceButBuildResolvable, agreeingRowTwo}

	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactIndexWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none", quarantined)
	}

	winner := index.images[digest]
	if got := singleSupplyChainImageSourceRepositoryID(winner); got != deployRepoID {
		t.Fatalf(
			"winner repository = %q, want the deploying repository %q: the row resolvable only via its own build provenance (factID %q, the smallest) must not outrank rows that already agree by source_repository_ids",
			got, deployRepoID, ambiguousSourceButBuildResolvable.FactID,
		)
	}
}

// TestPreferSupplyChainImageIdentityBuildProvenanceBeatsFullyUnresolved is the
// #5813 tier-B-over-tier-C guard: when every row for a digest is ambiguous or
// empty by sourceRepositoryIDs alone, a row that still resolves via its own
// buildProvenanceRepositoryIDs must win over a row that resolves via
// neither, EVEN when that build-provenance row has a LARGER factID (so the
// plain lexicographic tie-break, applied without the tier check, would have
// picked the wholly-unresolvable row instead and blanked the anchor).
func TestPreferSupplyChainImageIdentityBuildProvenanceBeatsFullyUnresolved(t *testing.T) {
	t.Parallel()

	const (
		digest      = "sha256:5813tierbc0000000000000000000000000000000000000000000000000000"
		buildRepoID = "repository:r_tier_b_builder"
		otherRepoID = "repository:r_tier_c_other"
	)

	// Tier B: ambiguous by sourceRepositoryIDs (names two repositories), but
	// buildProvenanceRepositoryIDs resolves to exactly one. Larger factID
	// than the tier C row below.
	tierBRow := containerImageIdentityImpactFactWithBuildProvenance(
		"identity-5813-z-tier-b", digest, "oci-registry://registry.example/tier-bc-app",
		buildRepoID, buildRepoID, otherRepoID,
	)
	// Tier C: ambiguous by sourceRepositoryIDs and carries no build
	// provenance at all -- wholly unresolvable. Smaller factID than tierBRow,
	// so an unqualified lexicographic tie-break would wrongly pick this row.
	tierCRow := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"identity-5813-a-tier-c", digest, "oci-registry://registry.example/tier-bc-app",
		buildRepoID, otherRepoID,
	)

	winner := preferSupplyChainImageIdentity(
		supplyChainImageIdentityFromEnvelope(tierCRow), supplyChainImageIdentityFromEnvelope(tierBRow),
	)
	if got := singleSupplyChainImageSourceRepositoryID(winner); got != buildRepoID {
		t.Fatalf(
			"preferSupplyChainImageIdentity winner repository = %q, want the build-provenance-resolvable repository %q: tier B must beat tier C even when tier C's factID sorts smaller",
			got, buildRepoID,
		)
	}

	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{tierCRow, tierBRow})
	if err != nil {
		t.Fatalf("buildSupplyChainImpactIndexWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none", quarantined)
	}
	if got := singleSupplyChainImageSourceRepositoryID(index.images[digest]); got != buildRepoID {
		t.Fatalf(
			"index.images[digest] repository = %q, want the build-provenance-resolvable repository %q: an all-ambiguous digest must not blank the anchor when one row's own build provenance can resolve it",
			got, buildRepoID,
		)
	}
}

// TestPreferSupplyChainImageIdentityAcceptedLimitationLoneDeployRowBeatsBuildProvenanceRow
// pins an ACCEPTED LIMITATION, not desired-in-principle behavior: see
// supplyChainImageIdentityAnchorTier's doc comment in
// supply_chain_impact_anchor_tier.go for the full explanation.
//
// Tier A (supplyChainImageIdentityAnchorTier) only requires that a row's OWN
// sourceRepositoryIDs is a singleton -- it does not require corroboration
// from any other row. So a single row carrying nothing but one weak,
// uncorroborated deploy/scope reference (tier A, because sourceRepositoryIDs
// is a singleton) outranks a row with genuine build provenance whose own
// sourceRepositoryIDs is ambiguous (tier B, resolvable only via
// buildProvenanceRepositoryIDs) -- even though the tier B row's evidence is
// individually stronger than the tier A row's.
//
// This is NOT a regression introduced by #5813's tier fix: pre-#5813 (and
// pre-#5801), preferSupplyChainImageIdentity's single "unambiguous" boolean
// was backed by singleSupplyChainImageSourceRepositoryID, which at that time
// reduced to the equivalent of len(sourceRepositoryIDs) == 1 (see
// git history for supply_chain_impact_index_build.go / supply_chain_impact_match.go
// pre-#5801) -- so the lone deploy-only row was ALREADY treated as
// unambiguous and ALREADY won over the build-provenance row in this exact
// shape, before either #5801 or #5813 touched this code. The tier fix
// deliberately preserves that outcome: tier B must never displace tier A,
// because the B-12 golden pin depends on tier A's priority holding even
// when tier A's own evidence is thin (fifteen independently agreeing deploy
// rows must keep beating one row that is ambiguous by sourceRepositoryIDs
// but resolvable via its own build provenance).
//
// Changing tier A to require multi-writer corroboration instead of mere
// singleton-ness of sourceRepositoryIDs is a deliberate semantic decision
// that needs owner sign-off -- it is not an incidental refactor. This test
// exists so such a change cannot silently flip this outcome unreviewed: a
// change that intentionally alters this behavior MUST update or replace this
// test as part of that sign-off, not accidentally break it.
func TestPreferSupplyChainImageIdentityAcceptedLimitationLoneDeployRowBeatsBuildProvenanceRow(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:5813accepted00000000000000000000000000000000000000000000000000"
		deployRepoID  = "repository:r_accepted_lone_deploy"
		buildRepoID   = "repository:r_accepted_build_evidence"
		staleRepoID   = "repository:r_accepted_stale_reference"
		loneDeployRow = "identity-5813-accepted-lone-deploy"
		buildRow      = "identity-5813-accepted-build-provenance"
	)

	// Tier A: a single row whose sourceRepositoryIDs carries exactly one
	// entry -- a weak deploy/scope reference with no corroboration from any
	// other row at all.
	loneDeployOnlyRow := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		loneDeployRow, digest, "oci-registry://registry.example/accepted-limitation-app", deployRepoID,
	)
	// Tier B: sourceRepositoryIDs is ambiguous (names the builder plus a
	// second, stale reference), but buildProvenanceRepositoryIDs resolves
	// unambiguously to the builder -- genuine, individually-strong evidence.
	buildProvenanceRow := containerImageIdentityImpactFactWithBuildProvenance(
		buildRow, digest, "oci-registry://registry.example/accepted-limitation-app",
		buildRepoID, buildRepoID, staleRepoID,
	)

	winner := preferSupplyChainImageIdentity(
		supplyChainImageIdentityFromEnvelope(loneDeployOnlyRow),
		supplyChainImageIdentityFromEnvelope(buildProvenanceRow),
	)
	if got := singleSupplyChainImageSourceRepositoryID(winner); got != deployRepoID {
		t.Fatalf(
			"preferSupplyChainImageIdentity winner repository = %q, want the lone deploy-only row's repository %q: "+
				"this pins the accepted pre-existing limitation where tier A's lack of multi-writer corroboration "+
				"lets a lone weak reference outrank genuine build provenance -- see supplyChainImageIdentityAnchorTier's doc comment",
			got, deployRepoID,
		)
	}
	if winner.factID != loneDeployRow {
		t.Fatalf("preferSupplyChainImageIdentity winner factID = %q, want the tier A row %q", winner.factID, loneDeployRow)
	}

	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(
		[]facts.Envelope{buildProvenanceRow, loneDeployOnlyRow},
	)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactIndexWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none", quarantined)
	}
	if got := singleSupplyChainImageSourceRepositoryID(index.images[digest]); got != deployRepoID {
		t.Fatalf(
			"index.images[digest] repository = %q, want the lone deploy-only row's repository %q (accepted limitation, see supplyChainImageIdentityAnchorTier)",
			got, deployRepoID,
		)
	}
}

// TestPreferSupplyChainImageIdentityConsensusSurvivesUnluckyFactIDDraw is the
// #5887 regression guard.
//
// #5854 made reducer_container_image_identity's fact ID outcome-independent
// (containerImageIdentityIdentity, container_image_identity_writer.go), which
// can collapse a scope's canonical decision down to a single
// source_repository_ids entry. That single-entry shape flips the row from
// tier B/C to tier A (supplyChainImageIdentityAnchorTier), and once every row
// for a digest is tier A, preferSupplyChainImageIdentity's same-tier
// tie-break falls back to comparing factID -- a SHA-256
// (facts.StableID/StableID) whose input embeds generation_id. Nine of the
// live corpus's twenty rows for digest sha256:abcdef...ab derive
// generation_id from GitCollectorSnapshotRun's wall-clock observed_at
// (go/internal/collector/git_source_processing.go: sourceRunID ->
// buildGeneration -> scope.ScopeGeneration.GenerationID), so their factIDs
// are a fresh, unpredictable draw on every collector run -- confirmed by
// reading that source, not assumed.
//
// This test forces one such unlucky draw directly rather than looping and
// hoping to catch a ~7%-of-runs flake: the lone build-repo row's factID is
// hardcoded as the global lexicographic minimum across all 20 rows, exactly
// reproducing a run where the per-run hash happened to sort that way. Before
// the #5887 fix, bestSupplyChainImageIdentitiesByDigest and
// buildSupplyChainImpactIndexWithQuarantine both let that one row win the
// digest purely on factID, anchoring every finding to the BUILDING
// repository instead of the deploying one. After the fix, corroboration
// count (nineteen deploy-repo rows agreeing vs. one build-repo row) decides
// the winner, so the deploy repo wins regardless of which way the per-run
// factID draw goes.
func TestPreferSupplyChainImageIdentityConsensusSurvivesUnluckyFactIDDraw(t *testing.T) {
	t.Parallel()

	const (
		digest       = "sha256:5887unlucky000000000000000000000000000000000000000000000000000"
		deployRepoID = "repository:r_217415d9"
		buildRepoID  = "repository:r_69256c06"
		decoy        = "oci-registry://registry.example/5887-unlucky-app"
	)

	// The lone build-repo row's factID is deliberately the GLOBAL MINIMUM
	// across all 20 rows -- the "unlucky draw" from #5887 where the per-run
	// generation_id hash happens to sort the CI/build row's factID below
	// every deploy-repo row's.
	ciRow := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"0000-5887-ci-row-wins-every-bare-lexicographic-tiebreak", digest, decoy, buildRepoID,
	)
	deployRows := make([]facts.Envelope, 0, 19)
	for i := 0; i < 19; i++ {
		deployRows = append(deployRows, containerImageIdentityImpactFactWithSourceRepositoryIDs(
			fmt.Sprintf("zzzz-5887-deploy-row-%02d", i), digest, decoy, deployRepoID,
		))
	}
	envelopes := append([]facts.Envelope{ciRow}, deployRows...)

	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactIndexWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none", quarantined)
	}

	winner := index.images[digest]
	if got := singleSupplyChainImageSourceRepositoryID(winner); got != deployRepoID {
		t.Fatalf(
			"index.images[digest] repository = %q, want the deploying repository %q: the lone build-repo row's factID %q sorts below all 19 deploy-repo rows' factIDs, so a bare lexicographic tie-break wrongly lets it win; corroboration (19 rows vs 1) must decide instead",
			got, deployRepoID, ciRow.FactID,
		)
	}

	// The batch helper must reach the identical winner.
	winners := bestSupplyChainImageIdentitiesByDigest(envelopes)
	if got := singleSupplyChainImageSourceRepositoryID(winners[digest]); got != deployRepoID {
		t.Fatalf("bestSupplyChainImageIdentitiesByDigest winner repository = %q, want %q", got, deployRepoID)
	}
}
