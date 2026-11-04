// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// retractedDLA converts a DLA that lists bullseye and marks its cve fact with
// status when status is non-empty, or sets the OSV withdrawn timestamp when it
// is empty. It returns the envelopes and the record's two fact ids.
func retractedDLA(t *testing.T, status string) ([]facts.Envelope, []string) {
	t.Helper()
	record := debianRecord("DLA-9999-1", "11", "bullseye", "7.74.0-1.3+deb11u10", "CVE-2023-38039")
	if status == "" {
		record.Withdrawn = "2026-01-02T00:00:00Z"
	}
	envelopes := convertedOSVRecord(t, record, false)
	var ids []string
	for at := range envelopes {
		switch envelopes[at].FactKind {
		case chain.VulnerabilityCVEFactKind:
			ids = append(ids, envelopes[at].FactID)
			if status != "" {
				envelopes[at].Payload["status"] = status
			}
		case chain.VulnerabilityAffectedPackageFactKind:
			ids = append(ids, envelopes[at].FactID)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("DLA fact ids = %v, want one cve and one affected_package fact", ids)
	}
	return envelopes, ids
}

// TestRetractedRecordDoesNotDecideALiveGroup: a withdrawn or rejected record
// beside a live record for the same package neither decides the verdict nor
// sets the fix, though its fact ids and status stay on the finding.
//
// Measured on the input below (bullseye host, live DEBIAN-CVE lists bookworm,
// withdrawn DLA lists bullseye): base e3d816172a read two findings, the
// DEBIAN-CVE one possibly_affected / os_release_not_in_advisory and the DLA one
// affected_exact with AdvisoryRetraction{withdrawn}. c35a68c258 read ONE finding,
// affected_exact from the withdrawn DLA, with no retraction and bookworm's range
// on a bullseye host.
func TestRetractedRecordDoesNotDecideALiveGroup(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	for name, status := range map[string]string{"withdrawn": "", "rejected": "Rejected"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dla, dlaFacts := retractedDLA(t, status)
			envelopes := append(upstreamEnvelopes(t, nil,
				debianRecord("DEBIAN-CVE-2023-38039", "12", "bookworm", "7.88.1-10+deb12u3", cve)), dla...)
			envelopes = append(envelopes, debianCurl("11", "7.74.0-1.3+deb11u7").envelope())
			for order, shuffled := range orderings(envelopes) {
				findings := BuildSupplyChainImpactFindings(shuffled)
				if len(findings) != 1 {
					t.Fatalf("%s: findings = %d, want 1: %#v", order, len(findings), findings)
				}
				got := findings[0]
				assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
				if got.MatchReason != "os_release_not_in_advisory" {
					t.Errorf("%s: MatchReason = %q, want os_release_not_in_advisory", order, got.MatchReason)
				}
				if got.AdvisoryRetraction != nil {
					t.Errorf("%s: AdvisoryRetraction = %#v, want nil: a live record still asserts the CVE", order, got.AdvisoryRetraction)
				}
				if got.FixedVersion != "" {
					t.Errorf("%s: FixedVersion = %q, want none: only the retracted record lists this line", order, got.FixedVersion)
				}
				for _, id := range dlaFacts {
					if !slices.Contains(got.EvidenceFactIDs, id) {
						t.Errorf("%s: evidence %v is missing the retracted record's fact %q", order, got.EvidenceFactIDs, id)
					}
				}
				if !slices.ContainsFunc(got.AdvisorySources, func(source AdvisorySourceObservation) bool {
					return source.AdvisoryID == "DLA-9999-1" && (source.WithdrawnAt != "" || source.Status != "")
				}) {
					t.Errorf("%s: AdvisorySources = %#v, want the DLA with its retraction", order, got.AdvisorySources)
				}
			}
		})
	}
}

// TestWhollyRetractedGroupKeepsItsVerdict is the control: with no live record
// the facts still decide and the finding carries the retraction, exactly as
// #9695 pinned for a single record.
func TestWhollyRetractedGroupKeepsItsVerdict(t *testing.T) {
	t.Parallel()

	for name, status := range map[string]string{"withdrawn": "", "rejected": "Rejected"} {
		dla, _ := retractedDLA(t, status)
		envelopes := append(dla, debianCurl("11", "7.74.0-1.3+deb11u7").envelope())
		for order, shuffled := range orderings(envelopes) {
			findings := BuildSupplyChainImpactFindings(shuffled)
			if len(findings) != 1 {
				t.Fatalf("%s %s: findings = %d, want 1: %#v", name, order, len(findings), findings)
			}
			got := findings[0]
			assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
			if got.AdvisoryID != "DLA-9999-1" || got.AdvisoryRetraction == nil {
				t.Errorf("%s %s: advisory/retraction = %q/%#v, want DLA-9999-1 with the retraction", name, order, got.AdvisoryID, got.AdvisoryRetraction)
			}
		}
	}
}

// TestLiveAdvisoryPackages pins the filter's edges: which record counts as
// retracted, when nothing is dropped, and that the input slice is left alone
// because the index shares it across installed rows.
func TestLiveAdvisoryPackages(t *testing.T) {
	t.Parallel()

	const withdrawn = "2026-01-02T00:00:00Z"
	pkgA := supplychainmodel.AffectedPackage{FactID: "pa", AdvisoryID: "A"}
	pkgB := supplychainmodel.AffectedPackage{FactID: "pb", AdvisoryID: "B"}
	tests := []struct {
		name        string
		cves        []supplychainmodel.ImpactCVE
		pkgs        []supplychainmodel.AffectedPackage
		wantPkgs    []string
		wantRetired []string
	}{
		{
			name:        "retracted record beside a live one is dropped",
			cves:        []supplychainmodel.ImpactCVE{{AdvisoryID: "A", WithdrawnAt: withdrawn}, {AdvisoryID: "B"}},
			pkgs:        []supplychainmodel.AffectedPackage{pkgA, pkgB},
			wantPkgs:    []string{"pb"},
			wantRetired: []string{"pa"},
		},
		{
			name:        "rejected status retracts as a withdrawn timestamp does",
			cves:        []supplychainmodel.ImpactCVE{{AdvisoryID: "A", Status: " REJECTED "}, {AdvisoryID: "B"}},
			pkgs:        []supplychainmodel.AffectedPackage{pkgA, pkgB},
			wantPkgs:    []string{"pb"},
			wantRetired: []string{"pa"},
		},
		{
			name:     "every record retracted keeps every fact",
			cves:     []supplychainmodel.ImpactCVE{{AdvisoryID: "A", WithdrawnAt: withdrawn}, {AdvisoryID: "B", Status: "rejected"}},
			pkgs:     []supplychainmodel.AffectedPackage{pkgA, pkgB},
			wantPkgs: []string{"pa", "pb"},
		},
		{
			name:     "a live source with no package fact does not count as a live package",
			cves:     []supplychainmodel.ImpactCVE{{AdvisoryID: "A", WithdrawnAt: withdrawn}, {AdvisoryID: "NVD-1"}},
			pkgs:     []supplychainmodel.AffectedPackage{pkgA},
			wantPkgs: []string{"pa"},
		},
		{
			name:     "a package with no cve fact is live, as retractionFromObservations reads it",
			cves:     []supplychainmodel.ImpactCVE{{AdvisoryID: "B", WithdrawnAt: withdrawn}},
			pkgs:     []supplychainmodel.AffectedPackage{pkgA, pkgB},
			wantPkgs: []string{"pa"}, wantRetired: []string{"pb"},
		},
		{
			name:     "one live cve fact of the record keeps it live",
			cves:     []supplychainmodel.ImpactCVE{{AdvisoryID: "A", WithdrawnAt: withdrawn}, {AdvisoryID: "A"}, {AdvisoryID: "B"}},
			pkgs:     []supplychainmodel.AffectedPackage{pkgA, pkgB},
			wantPkgs: []string{"pa", "pb"},
		},
		{
			name:     "no retraction at all returns the input",
			cves:     []supplychainmodel.ImpactCVE{{AdvisoryID: "A"}, {AdvisoryID: "B"}},
			pkgs:     []supplychainmodel.AffectedPackage{pkgA, pkgB},
			wantPkgs: []string{"pa", "pb"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := slices.Clone(tc.pkgs)
			group, got := liveAdvisoryPackages(supplyChainCVEGroup{observations: tc.cves}, input)
			if ids := packageFactIDs(got); !slices.Equal(ids, tc.wantPkgs) {
				t.Errorf("packages = %v, want %v", ids, tc.wantPkgs)
			}
			if !slices.Equal(group.retiredAffectedFactIDs, tc.wantRetired) {
				t.Errorf("retired = %v, want %v", group.retiredAffectedFactIDs, tc.wantRetired)
			}
			if !slices.Equal(packageFactIDs(input), packageFactIDs(tc.pkgs)) {
				t.Errorf("input slice changed to %v, want it untouched", packageFactIDs(input))
			}
		})
	}
}

// TestLiveAdvisoryPackagesDoesNotAllocateWithoutARetraction pins the hot path:
// this runs once per package group per finding pass, and almost no group has a
// retracted record.
func TestLiveAdvisoryPackagesDoesNotAllocateWithoutARetraction(t *testing.T) {
	group := supplyChainCVEGroup{observations: []supplychainmodel.ImpactCVE{{AdvisoryID: "A"}, {AdvisoryID: "B"}}}
	pkgs := []supplychainmodel.AffectedPackage{{FactID: "pa", AdvisoryID: "A"}, {FactID: "pb", AdvisoryID: "B"}}
	if allocs := testing.AllocsPerRun(100, func() { liveAdvisoryPackages(group, pkgs) }); allocs != 0 {
		t.Errorf("allocs = %v, want 0", allocs)
	}
}
