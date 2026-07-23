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
