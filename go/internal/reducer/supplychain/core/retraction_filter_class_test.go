// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The tests in this file pin that liveAdvisoryPackages reads a record's retraction
// the way retractedRecordOf does: a record is an advisory id within one source
// class. Two sources that share an advisory id (an NVD record and a vendor record
// both named by the CVE id) are two records, so one source's rejection must neither
// retire the other's live package fact nor leave a withdrawn fact deciding a live
// group (review 6, F6-1).

const (
	filterThirdAdvisory = "USN-LIVE"
	filterThirdVersion  = "9.9-1"
)

// filterThird is a live Ubuntu record whose fact lists a version the host does not
// run. It is the live record that makes the group non-wholly-retracted, so the
// filter runs; it does not hold the host.
func filterThird() []facts.Envelope {
	return []facts.Envelope{
		recordLive("cve-third", "ubuntu", filterThirdAdvisory),
		recordOSFact("fact-third", "ubuntu", filterThirdAdvisory, recordHostLine, filterThirdVersion, "1.0-25"),
	}
}

// TestFilterKeysRetractionBySourceClassAndAdvisoryID runs each row beside a control
// that differs only in the advisory id the retracted source uses, so the two sources
// no longer share an id. The row must read as its control in every fact order.
func TestFilterKeysRetractionBySourceClassAndAdvisoryID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// row and control hold the facts of the group; the host and the third record
		// are added to both.
		row, control []facts.Envelope
		// wantStatus pins what the control reads, so a control that drifts fails here
		// instead of agreeing with a wrong row.
		wantStatus SupplyChainImpactStatus
		wantAdvice string
	}{
		{
			// NVD rejects CVE-X; the vendor fact named CVE-X has no cve fact of its own and
			// holds the host. The rejection is NVD's, so the vendor fact stays live.
			name: "A1 an NVD rejection does not retire a vendor fact with the same id",
			row: []facts.Envelope{
				recordRejected("cve-nvd", "nvd", recordCVE),
				recordOSFact("fact-vendor", "ubuntu", recordCVE, recordHostLine, recordVersion, "1.0-15"),
			},
			control: []facts.Envelope{
				recordRejected("cve-nvd", "nvd", "OTHER"),
				recordOSFact("fact-vendor", "ubuntu", recordCVE, recordHostLine, recordVersion, "1.0-15"),
			},
			wantStatus: SupplyChainImpactAffectedExact, wantAdvice: recordCVE,
		},
		{
			// The vendor withdrew CVE-X and its fact holds the host; NVD still asserts CVE-X
			// live. The withdrawn fact must not decide the live group.
			name: "B2 a withdrawn vendor fact does not decide beside a live NVD record with the same id",
			row: []facts.Envelope{
				recordWithdrawn("cve-vendor", "ubuntu", recordCVE),
				recordOSFact("fact-vendor", "ubuntu", recordCVE, recordHostLine, recordVersion, "1.0-15"),
				recordLive("cve-nvd", "nvd", recordCVE),
			},
			control: []facts.Envelope{
				recordWithdrawn("cve-vendor", "ubuntu", "USN-W"),
				recordOSFact("fact-vendor", "ubuntu", "USN-W", recordHostLine, recordVersion, "1.0-15"),
				recordLive("cve-nvd", "nvd", recordCVE),
			},
			wantStatus: SupplyChainImpactPossiblyAffected, wantAdvice: filterThirdAdvisory,
		},
		{
			// The same as B2 with an OSV record.
			name: "B1 a withdrawn OSV fact does not decide beside a live NVD record with the same id",
			row: []facts.Envelope{
				recordWithdrawn("cve-osv", "osv", recordCVE),
				recordOSFact("fact-osv", "osv", recordCVE, recordHostLine, recordVersion, "1.0-15"),
				recordLive("cve-nvd", "nvd", recordCVE),
			},
			control: []facts.Envelope{
				recordWithdrawn("cve-osv", "osv", recordOSV),
				recordOSFact("fact-osv", "osv", recordOSV, recordHostLine, recordVersion, "1.0-15"),
				recordLive("cve-nvd", "nvd", recordCVE),
			},
			wantStatus: SupplyChainImpactPossiblyAffected, wantAdvice: filterThirdAdvisory,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base := slices.Concat(filterThird(), []facts.Envelope{recordHost()})
			control := oneRecordFinding(t, BuildSupplyChainImpactFindings(slices.Concat(tc.control, base)))
			if control.Status != tc.wantStatus || control.AdvisoryID != tc.wantAdvice {
				t.Fatalf("control = %s/%s/%s, want %s on %s", control.Status, control.MatchReason, control.AdvisoryID, tc.wantStatus, tc.wantAdvice)
			}
			for _, order := range everyOrder(slices.Concat(tc.row, base)) {
				findings := BuildSupplyChainImpactFindings(order)
				got := oneRecordFinding(t, findings)
				assertRetractedOffersNoFix(t, findings)
				if got.Status != control.Status || got.MatchReason != control.MatchReason ||
					got.AdvisoryID != control.AdvisoryID || got.FixedVersion != control.FixedVersion ||
					(got.AdvisoryRetraction == nil) != (control.AdvisoryRetraction == nil) {
					t.Fatalf("row reads %s/%s/%s fix %q retraction=%v, control %s/%s/%s fix %q retraction=%v",
						got.Status, got.MatchReason, got.AdvisoryID, got.FixedVersion, got.AdvisoryRetraction != nil,
						control.Status, control.MatchReason, control.AdvisoryID, control.FixedVersion, control.AdvisoryRetraction != nil)
				}
			}
		})
	}
}

// TestRecordClassIsTheClassifiedSourceNotTheLiteralSource pins that a package fact
// collected by osv under a GHSA id and a cve fact from the ghsa collector are one
// record: classifyAdvisorySource maps both to ghsa. The second package fact pairs
// with nothing, so only retractedRecordOf can give it its record's lifecycle, and it
// must do so whichever literal source name each fact carries (review 6, P3-4).
func TestRecordClassIsTheClassifiedSourceNotTheLiteralSource(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		recordWithdrawn("cve-1", "ghsa", recordGHSA),
		recordNPMFact("a", recordGHSA, "4.17.20", "4.17.21"),
		recordNPMFact("b", recordGHSA, "4.17.20", "4.17.22"),
		recordConsumer(),
	}
	for _, order := range everyOrder(input) {
		findings := BuildSupplyChainImpactFindings(order)
		got := oneRecordFinding(t, findings)
		assertRetractedOffersNoFix(t, findings)
		if got.AdvisoryRetraction == nil {
			t.Fatalf("no retraction: fix %q, sources %#v", got.FixedVersion, got.AdvisorySources)
		}
	}
}
