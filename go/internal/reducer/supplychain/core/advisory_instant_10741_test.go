// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychainmodel "github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestParseAdvisoryInstant10741 pins the shared advisory timestamp reader:
// RFC 3339 and the unzoned NVD shape (read as UTC) parse; everything else
// stays unparseable, which ranks oldest and earns no advisory age.
func TestParseAdvisoryInstant10741(t *testing.T) {
	t.Parallel()

	utc := func(y int, mo time.Month, d, h, mi, s, ns int) time.Time {
		return time.Date(y, mo, d, h, mi, s, ns, time.UTC)
	}
	tests := []struct {
		name   string
		raw    string
		want   time.Time
		wantOK bool
	}{
		{"zoned Z", "2026-01-02T00:00:00Z", utc(2026, 1, 2, 0, 0, 0, 0), true},
		{"zoned Z fraction", "2026-01-02T00:00:00.123456789Z", utc(2026, 1, 2, 0, 0, 0, 123456789), true},
		{"offset plus", "2026-01-02T01:00:00+01:00", utc(2026, 1, 2, 0, 0, 0, 0), true},
		{"offset minus", "2026-01-01T22:00:00-02:00", utc(2026, 1, 2, 0, 0, 0, 0), true},
		{"unzoned milliseconds NVD sample", "2026-08-11T19:33:44.513", utc(2026, 8, 11, 19, 33, 44, 513000000), true},
		{"unzoned milliseconds zero", "2026-01-02T00:00:00.000", utc(2026, 1, 2, 0, 0, 0, 0), true},
		{"unzoned published sample", "2021-12-10T10:15:09.143", utc(2021, 12, 10, 10, 15, 9, 143000000), true},
		{"unzoned no fraction", "2026-01-02T00:00:00", utc(2026, 1, 2, 0, 0, 0, 0), true},
		{"unzoned leading and trailing space", "  2026-01-02T00:00:00.000\t", utc(2026, 1, 2, 0, 0, 0, 0), true},
		{"far future unzoned", "2999-12-31T23:59:59.999", utc(2999, 12, 31, 23, 59, 59, 999000000), true},
		{"far future zoned", "9999-12-31T23:59:59Z", utc(9999, 12, 31, 23, 59, 59, 0), true},
		{"empty", "", time.Time{}, false},
		{"whitespace only", " \t ", time.Time{}, false},
		{"garbage", "not-a-time", time.Time{}, false},
		{"date only", "2026-05-20", time.Time{}, false},
		{"space separator", "2026-01-02 00:00:00", time.Time{}, false},
		{"space separator with zone", "2026-01-02 00:00:00Z", time.Time{}, false},
		{"hour 24 unzoned", "2026-01-02T24:00:00.000", time.Time{}, false},
		{"hour 24 zoned", "2026-01-02T24:00:00Z", time.Time{}, false},
		{"month 13", "2026-13-02T00:00:00", time.Time{}, false},
		{"hour minute only", "2026-01-02T00:00", time.Time{}, false},
		{"trailing garbage", "2026-01-02T00:00:00.000 UTC", time.Time{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseAdvisoryInstant(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("parseAdvisoryInstant(%q) ok = %v, want %v (got %v)", tc.raw, ok, tc.wantOK, got)
			}
			if !ok {
				if !got.IsZero() {
					t.Fatalf("parseAdvisoryInstant(%q) = %v on failure, want the zero time", tc.raw, got)
				}
				return
			}
			if !got.Equal(tc.want) {
				t.Fatalf("parseAdvisoryInstant(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseAdvisoryInstantSameInstantTwoShapesCompareEqual10741 pins that one
// instant in the unzoned and zoned shapes is Equal, so a collector that later
// normalizes to Z changes no election outcome.
func TestParseAdvisoryInstantSameInstantTwoShapesCompareEqual10741(t *testing.T) {
	t.Parallel()

	unzoned, ok := parseAdvisoryInstant("2026-01-02T00:00:00.000")
	if !ok {
		t.Fatal("unzoned stamp did not parse")
	}
	zoned, ok := parseAdvisoryInstant("2026-01-02T00:00:00Z")
	if !ok {
		t.Fatal("zoned stamp did not parse")
	}
	if !unzoned.Equal(zoned) {
		t.Fatalf("unzoned %v and zoned %v differ, want Equal", unzoned, zoned)
	}
	offset, ok := parseAdvisoryInstant("2026-01-01T23:00:00-02:00")
	if !ok {
		t.Fatal("offset stamp did not parse")
	}
	if !offset.After(unzoned) {
		t.Fatalf("offset copy %v must rank after the unzoned %v (it is 01:00Z)", offset, unzoned)
	}
}

// TestNewestCopyAfterSameInstantTwoShapesTies10741 pins the tie path for one
// instant in two shapes: neither copy is newer by time, so the existing
// live-beats-retracted rule decides, then the greater FactID. The recency
// key never splits the pair.
func TestNewestCopyAfterSameInstantTwoShapesTies10741(t *testing.T) {
	t.Parallel()

	unzonedLive := supplychainmodel.ImpactCVE{FactID: "fact-a", SourceUpdatedAt: "2026-01-02T00:00:00.000", Status: "Analyzed"}
	zonedLive := supplychainmodel.ImpactCVE{FactID: "fact-b", SourceUpdatedAt: "2026-01-02T00:00:00Z", Status: "Analyzed"}
	zonedDead := supplychainmodel.ImpactCVE{FactID: "fact-c", SourceUpdatedAt: "2026-01-02T00:00:00Z", Status: "Rejected"}
	unzonedDead := supplychainmodel.ImpactCVE{FactID: "fact-d", SourceUpdatedAt: "2026-01-02T00:00:00.000", Status: "Rejected"}

	// Same lifecycle: the greater FactID wins whichever shape it carries.
	if !newestCopyAfter(zonedLive, unzonedLive) || newestCopyAfter(unzonedLive, zonedLive) {
		t.Fatal("equal instants, same lifecycle: want the greater FactID to win in both argument orders")
	}
	// Divergent lifecycle: live beats retracted although the retracted FactID is greater.
	if !newestCopyAfter(unzonedLive, zonedDead) || newestCopyAfter(zonedDead, unzonedLive) {
		t.Fatal("equal instants, divergent lifecycle: want the live copy to win in both argument orders")
	}
	if !newestCopyAfter(zonedLive, unzonedDead) || newestCopyAfter(unzonedDead, zonedLive) {
		t.Fatal("equal instants, divergent lifecycle: want the live copy to win in both argument orders")
	}
}

// TestNewestCopyElectionSameInstantTwoShapesStaysActive10741 runs the same
// tie through the full admission path: a Rejected copy and an Analyzed copy
// with one instant in two shapes leave the finding active in every load order.
func TestNewestCopyElectionSameInstantTwoShapesStaysActive10741(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-9745"
	in := []facts.Envelope{
		newestCopyCVE("cve-a-live", "scope:w1", cveID, "nvd", cveID, 5.0, "2026-01-02T00:00:00.000", "Analyzed", ""),
		newestCopyPackage("pkg-a-live", "scope:w1", cveID, "nvd", cveID, "1.0.2"),
		newestCopyCVE("cve-z-dead", "scope:w2", cveID, "nvd", cveID, 9.8, "2026-01-02T00:00:00Z", "Rejected", ""),
		newestCopyPackage("pkg-z-dead", "scope:w2", cveID, "nvd", cveID, "1.0.1"),
		retractionConsumption(),
	}
	for i, order := range permutations(in) {
		finding := newestCopyFinding(t, order)
		if finding.Suppression.State != SupplyChainSuppressionStateActive || finding.CVSSScore != 5.0 {
			t.Fatalf("permutation %d %v: state=%q score=%v, want active with the live copy's 5.0",
				i, factIDs(order), finding.Suppression.State, finding.CVSSScore)
		}
	}
}
