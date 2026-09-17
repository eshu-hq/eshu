// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImageAnchorNeverEmptyWhenResolvableEvidencePresent is the
// #6702 Prove-The-Theory-First shim, kept as a committed regression guard.
//
// #6702 observes a finding with an empty repository_id anchor on ~1/60 gate
// runs and suspects tier-B handling in buildSupplyChainImageIdentityConsensus
// / singleSupplyChainImageSourceRepositoryID. This test settles the tier half
// of that suspicion for the live corpus shape (nineteen rows agreeing on the
// deploying repository via source_repository_ids alone plus one CI-scope row
// naming the building repository): across adversarial factID draws (the
// per-run generation_id redraw #5887 describes) and across envelope arrival
// orders, the consensus winner must resolve to the deploying repository --
// never to empty. A green run exonerates the tier tie-break for this shape:
// an empty winner then requires zero resolvable rows in the batch (evidence
// absent at load time), not a tier mis-vote among rows present.
func TestSupplyChainImageAnchorNeverEmptyWhenResolvableEvidencePresent(t *testing.T) {
	t.Parallel()

	const (
		digest     = "sha256:6702000000000000000000000000000000000000000000000000000000000000"
		deployRepo = "repository:r_217415d9"
		buildRepo  = "repository:r_69256c06"
		decoy      = "oci-registry://registry.example/6702-anchor-stability-app"
	)

	buildCorpusShape := func(drawPrefix string) []facts.Envelope {
		envelopes := []facts.Envelope{
			containerImageIdentityImpactFactWithSourceRepositoryIDs(
				drawPrefix+"-ci-row", digest, decoy, buildRepo,
			),
		}
		for i := 0; i < 19; i++ {
			envelopes = append(envelopes,
				containerImageIdentityImpactFactWithSourceRepositoryIDs(
					fmt.Sprintf("%s-deploy-%02d", drawPrefix, i), digest, decoy, deployRepo,
				),
			)
		}
		return envelopes
	}

	orders := map[string]func([]facts.Envelope) []facts.Envelope{
		"arrival": func(in []facts.Envelope) []facts.Envelope { return in },
		"reversed": func(in []facts.Envelope) []facts.Envelope {
			out := make([]facts.Envelope, len(in))
			for i, envelope := range in {
				out[len(in)-1-i] = envelope
			}
			return out
		},
		"rotated": func(in []facts.Envelope) []facts.Envelope {
			out := make([]facts.Envelope, 0, len(in))
			return append(append(out, in[7:]...), in[:7]...)
		},
	}

	// "0000" puts the lone build-repo row's factID smallest (the unlucky
	// draw); "zzzz" puts a deploy row smallest. Pre-#5887 bare factID logic
	// picked different repositories between such draws.
	for _, drawPrefix := range []string{"0000-draw", "zzzz-draw"} {
		for orderName, reorder := range orders {
			winners := bestSupplyChainImageIdentitiesByDigest(reorder(buildCorpusShape(drawPrefix)))
			winner, ok := winners[digest]
			if !ok {
				t.Fatalf("draw %q order %s: no winner for digest", drawPrefix, orderName)
			}
			if got := singleSupplyChainImageSourceRepositoryID(winner); got != deployRepo {
				t.Fatalf(
					"draw %q order %s: resolved repository = %q, want %q (empty would reproduce #6702 at the tier layer)",
					drawPrefix, orderName, got, deployRepo,
				)
			}
		}
	}
}

// TestSupplyChainImageAnchorEmptyOnlyWhenNothingResolves pins the boundary of
// the test above: the tier layer yields an empty winner if and only if every
// row for the digest is tier C (ambiguous or empty source with no
// single-valued build provenance). Any future #6702 fix that changes what an
// all-ambiguous batch resolves to must update this test deliberately.
func TestSupplyChainImageAnchorEmptyOnlyWhenNothingResolves(t *testing.T) {
	t.Parallel()

	const (
		digest     = "sha256:6702000000000000000000000000000000000000000000000000000000000001"
		deployRepo = "repository:r_217415d9"
		buildRepo  = "repository:r_69256c06"
		decoy      = "oci-registry://registry.example/6702-anchor-ambiguous-app"
	)

	ambiguous := []facts.Envelope{
		containerImageIdentityImpactFactWithSourceRepositoryIDs(
			"ambiguous-1", digest, decoy, deployRepo, buildRepo,
		),
		containerImageIdentityImpactFactWithSourceRepositoryIDs(
			"ambiguous-2", digest, decoy, buildRepo, deployRepo,
		),
	}
	winners := bestSupplyChainImageIdentitiesByDigest(ambiguous)
	winner, ok := winners[digest]
	if !ok {
		t.Fatalf("no winner for digest with only ambiguous rows")
	}
	if got := singleSupplyChainImageSourceRepositoryID(winner); got != "" {
		t.Fatalf("all-ambiguous batch resolved %q, want empty (never invent an anchor, #5463)", got)
	}

	singleResolvable := append(append([]facts.Envelope(nil), ambiguous...),
		containerImageIdentityImpactFactWithSourceRepositoryIDs(
			"resolvable-1", digest, decoy, deployRepo,
		),
	)
	winners = bestSupplyChainImageIdentitiesByDigest(singleResolvable)
	if got := singleSupplyChainImageSourceRepositoryID(winners[digest]); got != deployRepo {
		t.Fatalf("one resolvable row among ambiguous rows resolved %q, want %q", got, deployRepo)
	}
}
