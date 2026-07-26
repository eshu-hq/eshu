// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactIndexContainerImageIdentityDeterministicAcrossEnvelopeOrder
// is the determinism regression guard for #5464 layer 2: the producer writes
// one reducer_container_image_identity row per triggering scope/ref with no
// per-digest canonicalization, so a single digest can carry many rows that
// disagree on source_repository_ids (this repo's own live corpus has 11 rows
// for one digest, ten naming one repository and one naming two). Both
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
	// repository -- mirroring the corpus's ten agreeing rows -- and
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
// corpus shape for digest sha256:abcdef...ab -- ten rows agreeing on the
// DEPLOYING repository via sourceRepositoryIDs alone, and one row that names
// BOTH the deploying repository and its own building repository in
// sourceRepositoryIDs (making sourceRepositoryIDs ambiguous for that row) but
// resolves unambiguously via buildProvenanceRepositoryIDs.
//
// Before #5813's fix, preferSupplyChainImageIdentity's single "unambiguous"
// boolean treated singleSupplyChainImageSourceRepositoryID's row-level
// provenance-first result as the ENTIRE preference signal, so the
// two-repository row (rendered "unambiguous" only via its own build
// evidence) competed on equal footing with -- and won over -- the ten
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
