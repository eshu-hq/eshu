// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// These tests pin #10741: NVD API 2.0 reports lastModified and published as
// ISO 8601 seconds-precision values with no zone designator (for example
// "2026-08-11T19:33:44.513"), and the collector copies them verbatim into
// modified_at / published_at. NVD emits UTC, so the reducer reads an unzoned
// value as UTC. Before the fix, time.Parse(time.RFC3339) rejected every NVD
// stamp, both copies of a record ranked oldest, the election fell to the tie
// path, and an older Analyzed copy could beat a newer Rejected copy by FactID.
//
// RED on origin/main: TestNewestCopyUnzonedNVDRetractsOlderActiveCopy10741
// (the unzoned rows) and TestAdvisoryAgeReadsUnzonedNVDTimestamps10741.

const (
	// nvdShapedOlder and nvdShapedNewer are NVD-shaped unzoned stamps, the
	// millisecond form the live API returns.
	nvdShapedOlder = "2026-10-01T00:00:00.000"
	nvdShapedNewer = "2026-11-01T00:00:00.513"
)

// TestNewestCopyUnzonedNVDRetractsOlderActiveCopy10741 is the issue repro: an
// older Analyzed copy and a newer Rejected copy of one NVD record, in two
// window scopes, retract the finding in every load order. The Z row is the
// control that already passes; the unzoned and mixed rows are RED today.
func TestNewestCopyUnzonedNVDRetractsOlderActiveCopy10741(t *testing.T) {
	t.Parallel()

	shapes := []struct {
		name                 string
		olderTime, newerTime string
	}{
		{"zoned control", newestCopyOlder, newestCopyNewer},
		{"unzoned milliseconds", nvdShapedOlder, nvdShapedNewer},
		{"unzoned no fraction", "2026-10-01T00:00:00", "2026-11-01T00:00:00"},
		{"unzoned older, zoned newer", nvdShapedOlder, newestCopyNewer},
		{"zoned older, unzoned newer", newestCopyOlder, nvdShapedNewer},
	}
	for _, shape := range shapes {
		for _, order := range []string{"older-first", "newer-first"} {
			t.Run(shape.name+"/"+order, func(t *testing.T) {
				t.Parallel()
				const cveID = "CVE-2026-9741"
				// The older Analyzed copy holds the GREATER FactID: when the
				// timestamps do not parse, the FactID tiebreak makes it win
				// and the finding wrongly stays active.
				older := []facts.Envelope{
					newestCopyCVE("cve-z-older", "scope:w1", cveID, "nvd", cveID, 9.8, shape.olderTime, "Analyzed", ""),
					newestCopyPackage("pkg-z-older", "scope:w1", cveID, "nvd", cveID, "1.0.1"),
				}
				newer := []facts.Envelope{
					newestCopyCVE("cve-a-newer", "scope:w2", cveID, "nvd", cveID, 9.8, shape.newerTime, "Rejected", ""),
					newestCopyPackage("pkg-a-newer", "scope:w2", cveID, "nvd", cveID, "1.0.1"),
				}
				in := append(append([]facts.Envelope(nil), older...), newer...)
				if order == "newer-first" {
					in = append(append([]facts.Envelope(nil), newer...), older...)
				}
				in = append(in, retractionConsumption())

				finding := newestCopyFinding(t, in)
				if got := finding.Suppression.State; got != SupplyChainSuppressionStateAdvisoryRetracted {
					t.Fatalf("state = %q, want advisory_retracted: the newer Rejected copy is the record", got)
				}
				if finding.AdvisoryRetraction == nil || finding.AdvisoryRetraction.ReasonCode != AdvisoryRetractionRejected {
					t.Fatalf("AdvisoryRetraction = %#v, want a rejected retraction", finding.AdvisoryRetraction)
				}
			})
		}
	}
}

// TestNewestCopyUnzonedNVDNewerAnalyzedStaysActive10741 is the opposite
// direction: when the older copy is the Rejected one and the newer copy is
// Analyzed, the newer live copy is the record and the finding stays active.
// With unparseable stamps the greater FactID (the Rejected copy here) would
// win a tie only if the live-beats-retracted rule did not apply, so this row
// also pins that parsing the stamps does not invert the election.
func TestNewestCopyUnzonedNVDNewerAnalyzedStaysActive10741(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-9742"
	in := []facts.Envelope{
		newestCopyCVE("cve-z-older", "scope:w1", cveID, "nvd", cveID, 9.8, nvdShapedOlder, "Rejected", ""),
		newestCopyPackage("pkg-z-older", "scope:w1", cveID, "nvd", cveID, "1.0.1"),
		newestCopyCVE("cve-a-newer", "scope:w2", cveID, "nvd", cveID, 5.0, nvdShapedNewer, "Analyzed", ""),
		newestCopyPackage("pkg-a-newer", "scope:w2", cveID, "nvd", cveID, "1.0.2"),
		retractionConsumption(),
	}
	for i, order := range permutations(in) {
		finding := newestCopyFinding(t, order)
		if finding.Suppression.State != SupplyChainSuppressionStateActive || finding.AdvisoryRetraction != nil {
			t.Fatalf("permutation %d %v: state=%q retraction=%v, want active", i, factIDs(order),
				finding.Suppression.State, finding.AdvisoryRetraction)
		}
		if finding.CVSSScore != 5.0 || finding.FixedVersion != "1.0.2" {
			t.Fatalf("permutation %d %v: score=%v fixed=%q, want the newer copy's 5.0, 1.0.2",
				i, factIDs(order), finding.CVSSScore, finding.FixedVersion)
		}
	}
}

// TestNewestCopySingleCopyUnzonedUnchanged10741 pins that a single-copy
// advisory never reaches the election (the newestCopyAdvisoryInputs fast
// path): a lone Rejected NVD copy retracts and a lone Analyzed copy stays
// active whatever its timestamp shape, including an empty or garbage one.
func TestNewestCopySingleCopyUnzonedUnchanged10741(t *testing.T) {
	t.Parallel()

	for _, stamp := range []string{nvdShapedNewer, newestCopyNewer, "", "not-a-time", "2026-05-20"} {
		for _, tc := range []struct {
			status    string
			wantState SupplyChainSuppressionState
		}{
			{"Rejected", SupplyChainSuppressionStateAdvisoryRetracted},
			{"Analyzed", SupplyChainSuppressionStateActive},
		} {
			t.Run(tc.status+"/"+stamp, func(t *testing.T) {
				t.Parallel()
				const cveID = "CVE-2026-9743"
				finding := newestCopyFinding(t, []facts.Envelope{
					newestCopyCVE("cve-only", "scope:w1", cveID, "nvd", cveID, 9.8, stamp, tc.status, ""),
					newestCopyPackage("pkg-only", "scope:w1", cveID, "nvd", cveID, "1.0.1"),
					retractionConsumption(),
				})
				if got := finding.Suppression.State; got != tc.wantState {
					t.Fatalf("state = %q, want %q for a single copy", got, tc.wantState)
				}
			})
		}
	}
}

// TestAdvisoryAgeReadsUnzonedNVDTimestamps10741 pins the accepted side
// effect: NVD published/modified stamps now parse, so an NVD-sourced finding
// carries an advisory_age contribution it never had. RED today for the
// unzoned rows (no contribution at all).
func TestAdvisoryAgeReadsUnzonedNVDTimestamps10741(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name               string
		published, updated string
		wantCode           string
	}{
		{"zoned control", "2026-05-01T00:00:00Z", "2026-05-10T00:00:00Z", "advisory_age_recent"},
		{"unzoned recent", "2026-05-01T00:00:00.000", "2026-05-10T00:00:00.513", "advisory_age_recent"},
		{"unzoned maturing", "2026-01-01T00:00:00.000", "2026-05-10T00:00:00.000", "advisory_age_maturing"},
		{"unzoned mature", "2021-12-10T10:15:09.143", "2026-08-11T19:33:44.513", "advisory_age_mature"},
		{"mixed shapes", "2026-05-01T00:00:00.000", "2026-05-10T00:00:00Z", "advisory_age_recent"},
		{"unparseable contributes nothing", "not-a-time", "2026-05-10T00:00:00.000", ""},
		{"date-only contributes nothing", "2026-05-01", "2026-05-10T00:00:00.000", ""},
		{"updated before published contributes nothing", "2026-05-10T00:00:00.000", "2026-05-01T00:00:00.000", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const cveID = "CVE-2026-9744"
			finding := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings([]facts.Envelope{
				vulnerabilityCVEFactWithDates("cve-age", cveID, 9.8, "", tc.published, tc.updated),
				vulnerabilityAffectedPackageFact("affected-age", cveID, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
				packageConsumptionFactWithChain("consume-age", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"example"}, 1, true),
			}))[cveID]
			var ageCodes []string
			for _, code := range finding.PriorityReasonCodes {
				if strings.HasPrefix(code, "advisory_age_") {
					ageCodes = append(ageCodes, code)
				}
			}
			want := []string(nil)
			if tc.wantCode != "" {
				want = []string{tc.wantCode}
			}
			if !slices.Equal(ageCodes, want) {
				t.Fatalf("advisory_age codes = %v, want %v (reasons %v)", ageCodes, want, finding.PriorityReasonCodes)
			}
		})
	}
}
