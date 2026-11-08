// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// These tests pin the #10401 redesign ruling for the newest-copy election in
// newest_copy.go: on any recency tie — equal instants, or both empty or
// unparseable — a live copy beats a retracted copy, and the greater FactID is
// only the final deterministic tiebreak. The rationale is fail-visible: a
// FactID order is hash-derived and semantically a coin flip, so hiding a live
// vulnerability on zero evidence spends the expensive error.
//
// RED on the pre-#10401 rule: ties break on the greater FactID with no
// lifecycle signal, so the withdrawn/Rejected copy below (which always holds
// the greater FactID) wins the election and the finding retracts. The winner
// fields must read the live copy, in every load order.

// TestNewestCopyDivergentTieLiveBeatsRetracted10401 covers the #10401
// acceptance rows: a withdrawn or Rejected copy holding the GREATER FactID
// loses a recency tie to the live copy, for every producer variant and every
// tie shape (equal instants, both empty, both unparseable, empty versus
// unparseable). The finding stays active and carries the live copy's
// severity, fixed version, and range.
func TestNewestCopyDivergentTieLiveBeatsRetracted10401(t *testing.T) {
	t.Parallel()

	ties := []struct {
		name               string
		liveTime, deadTime string
	}{
		{"equal instants", newestCopyNewer, newestCopyNewer},
		{"both empty", "", ""},
		{"both unparseable", "not-a-time", "also-not-a-time"},
		{"empty versus unparseable", "", "not-a-time"},
	}
	cases := []struct {
		name       string
		source     string
		advisoryID string
		cveID      string
		status     string
		withdrawn  string
	}{
		{"ghsa withdrawn_at", "ghsa", "GHSA-aaaa-bbbb-cccc", "CVE-2026-9701", "", newestCopyNewer},
		{"ghsa status withdrawn", "ghsa", "GHSA-aaaa-bbbb-cccc", "CVE-2026-9702", "withdrawn", ""},
		{"osv-carried ghsa withdrawn_at", "osv", "GHSA-dddd-eeee-ffff", "CVE-2026-9703", "", newestCopyNewer},
		{"nvd Rejected", "nvd", "CVE-2026-9704", "CVE-2026-9704", "Rejected", ""},
	}
	for _, tc := range cases {
		for _, tie := range ties {
			t.Run(tc.name+"/"+tie.name, func(t *testing.T) {
				t.Parallel()
				// The withdrawn/Rejected copy holds the greater FactID on
				// purpose: under the pre-#10401 FactID tiebreak it wins.
				live := []facts.Envelope{
					newestCopyCVE("fact-a-live", "scope:a", tc.cveID, tc.source, tc.advisoryID, 5.0, tie.liveTime, "", ""),
					newestCopyPackage("pkg-a-live", "scope:a", tc.cveID, tc.source, tc.advisoryID, "1.0.2"),
				}
				dead := []facts.Envelope{
					newestCopyCVE("fact-z-dead", "scope:b", tc.cveID, tc.source, tc.advisoryID, 9.8, tie.deadTime, tc.status, tc.withdrawn),
					newestCopyPackage("pkg-z-dead", "scope:b", tc.cveID, tc.source, tc.advisoryID, "1.0.1"),
				}
				all := append(append(append([]facts.Envelope(nil), live...), dead...), retractionConsumption())
				for i, order := range permutations(all) {
					finding := newestCopyFinding(t, order)
					if finding.Suppression.State != SupplyChainSuppressionStateActive || finding.AdvisoryRetraction != nil {
						t.Fatalf("permutation %d %v: state=%q retraction=%v, want active (the live copy wins the tie)",
							i, factIDs(order), finding.Suppression.State, finding.AdvisoryRetraction)
					}
					if finding.CVSSScore != 5.0 || finding.FixedVersion != "1.0.2" || finding.VulnerableRange != ">=1.0.0 <1.0.2" {
						t.Fatalf("permutation %d %v: score=%v fixed=%q range=%q, want the live copy's 5.0, 1.0.2, >=1.0.0 <1.0.2",
							i, factIDs(order), finding.CVSSScore, finding.FixedVersion, finding.VulnerableRange)
					}
				}
			})
		}
	}
}

// TestNewestCopyRetractedVsRetractedTieStaysFactIDOrdered10401 is the guard
// row with a withdrawn copy holding the greater FactID: when BOTH copies of
// a tie are retracted, the greater FactID still wins deterministically and
// the finding retracts. GREEN before and after #10401: it pins that the new
// live-beats-retracted rank only breaks divergent ties and never disturbs
// retracted-versus-retracted determinism.
func TestNewestCopyRetractedVsRetractedTieStaysFactIDOrdered10401(t *testing.T) {
	t.Parallel()

	const (
		cveID      = "CVE-2026-9711"
		advisoryID = "GHSA-wwww-xxxx-yyyy"
	)
	older := []facts.Envelope{
		newestCopyCVE("fact-a-dead", "scope:a", cveID, "ghsa", advisoryID, 9.8, newestCopyNewer, "", newestCopyOlder),
		newestCopyPackage("pkg-a-dead", "scope:a", cveID, "ghsa", advisoryID, "1.0.1"),
	}
	newer := []facts.Envelope{
		newestCopyCVE("fact-z-dead", "scope:b", cveID, "ghsa", advisoryID, 5.0, newestCopyNewer, "", newestCopyNewer),
		newestCopyPackage("pkg-z-dead", "scope:b", cveID, "ghsa", advisoryID, "1.0.2"),
	}
	all := append(append(append([]facts.Envelope(nil), older...), newer...), retractionConsumption())
	for i, order := range permutations(all) {
		finding := newestCopyFinding(t, order)
		if got := finding.Suppression.State; got != SupplyChainSuppressionStateAdvisoryRetracted {
			t.Fatalf("permutation %d %v: state=%q, want advisory_retracted (both copies retracted)",
				i, factIDs(order), got)
		}
		if finding.AdvisoryRetraction == nil {
			t.Fatalf("permutation %d %v: AdvisoryRetraction = nil, want a withdrawn retraction", i, factIDs(order))
		}
		if finding.AdvisoryRetraction.ReasonCode != AdvisoryRetractionWithdrawn {
			t.Fatalf("permutation %d %v: ReasonCode=%q, want withdrawn",
				i, factIDs(order), finding.AdvisoryRetraction.ReasonCode)
		}
		if got := len(finding.AdvisoryRetraction.Sources); got != 1 {
			t.Fatalf("permutation %d %v: retraction sources=%d, want 1 (one deterministic winner)", i, factIDs(order), got)
		}
		// The two copies carry different withdrawn_at markers, so the
		// surviving source names the winner: the greater FactID on a tie.
		if got := finding.AdvisoryRetraction.Sources[0].WithdrawnAt; got != newestCopyNewer {
			t.Fatalf("permutation %d %v: winner withdrawn_at=%q, want %q (the greater FactID's copy)",
				i, factIDs(order), got, newestCopyNewer)
		}
	}
}
