// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// RED for the #9914 dpkg-spelling limit tracked in #10034: OS spellings
// outside the vendor-class set ("dpkg") get neither the 9914-r5
// retraction-anchor rule nor the N1/N2 record rules, because
// osPackageClassEcosystem does not know the spelling. A "dpkg"-ecosystem
// group must read exactly as its "os" twin: the withdrawn record names the
// finding with its retraction shown.

// dpkgRecordFact is recordOSFact spelled "dpkg".
func dpkgRecordFact(id, source, advisory, line, version string, fix ...any) facts.Envelope {
	envelope := recordOSFact(id, source, advisory, line, version, fix...)
	envelope.Payload["ecosystem"] = "dpkg"
	return envelope
}

// TestDpkgSpellingReadsTheOSRetractionRule pins the limit: a withdrawn USN
// beside a live NVD cve-only record, all facts spelled dpkg, reads the N2
// answer (vendor naming with retraction), not the language-class answer.
func TestDpkgSpellingReadsTheOSRetractionRule(t *testing.T) {
	t.Parallel()
	dpkg := func(id string, fix ...any) facts.Envelope {
		return dpkgRecordFact(id, "ubuntu", recordVendor, recordHostLine, recordVersion, fix...)
	}
	withdrawn := recordWithdrawn("cve-own", "ubuntu", recordVendor)
	liveNVD := recordLive("nvd", "nvd", recordCVE)
	row := []facts.Envelope{withdrawn, liveNVD, recordHost(), dpkg("a", "1.0-5"), dpkg("b", "1.0-6")}
	twin := []facts.Envelope{withdrawn, liveNVD, recordHost(), recordVendorFact("a", recordHostLine, recordVersion, "1.0-5")}

	control := oneRecordFinding(t, BuildSupplyChainImpactFindings(slices.Clone(twin)))
	if control.AdvisoryID != recordVendor || control.AdvisoryRetraction == nil {
		t.Fatalf("control = %q retraction %#v, want %q with a retraction", control.AdvisoryID, control.AdvisoryRetraction, recordVendor)
	}
	for _, order := range everyOrder(row) {
		got := oneRecordFinding(t, BuildSupplyChainImpactFindings(order))
		assertRetractedOffersNoFix(t, []SupplyChainImpactFinding{got})
		if got.AdvisoryID != recordVendor || got.AdvisoryRetraction == nil {
			t.Fatalf("order %s: advisory/retraction = %q/%#v, want %q with a retraction",
				repDirectOrderLabel(order), got.AdvisoryID, got.AdvisoryRetraction, recordVendor)
		}
		if got.Status != control.Status || got.MatchReason != control.MatchReason {
			t.Errorf("order %s: status/reason = %s/%s, control %s/%s", repDirectOrderLabel(order),
				got.Status, got.MatchReason, control.Status, control.MatchReason)
		}
	}
}

// TestDpkgSpellingKeepsTheRetractionAnchor pins the r5 half: when only the
// retracted record anchors the finding, the dpkg group reads the retracted
// record's finding with the retraction shown, as the "os" twin does.
func TestDpkgSpellingKeepsTheRetractionAnchor(t *testing.T) {
	t.Parallel()
	liveExt := recordLive("cve-other", "nvd", "EXT-1")
	livePkg := dpkgRecordFact("affected-other", "nvd", "EXT-1", recordHostLine, recordVersion, "1.0-2")
	livePkg.Payload["ecosystem"] = "os"
	row := []facts.Envelope{
		recordWithdrawn("cve-own", "ubuntu", recordVendor), liveExt, livePkg, recordHost(),
		dpkgRecordFact("a", "ubuntu", recordVendor, recordHostLine, recordVersion, "1.0-5"),
		dpkgRecordFact("b", "ubuntu", recordVendor, recordHostLine, recordVersion, "1.0-6"),
	}
	var first []SupplyChainImpactFinding
	for _, order := range everyOrder(row) {
		findings := BuildSupplyChainImpactFindings(order)
		got := oneRecordFinding(t, findings)
		assertRetractedOffersNoFix(t, findings)
		if first == nil {
			first = findings
		}
		if got.AdvisoryID != recordVendor || got.AdvisoryRetraction == nil {
			t.Fatalf("order %s: advisory/retraction = %q/%#v, want %q with a retraction",
				repDirectOrderLabel(order), got.AdvisoryID, got.AdvisoryRetraction, recordVendor)
		}
	}
}
