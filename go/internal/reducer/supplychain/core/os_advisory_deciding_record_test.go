// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSelectAdvisoryProvenanceNamesTheDecidingRecordForOSClass: for an OS-class
// package the record that supplied the fact the version decision read first
// names the finding. A scored source still supplies the score; it does not
// rename the finding. The ecosystems are the OS-class values the code reads:
// "os" is what the OSV converter emits (os://debian/curl), the rest are the
// vendorForOSPackageClass keys.
func TestSelectAdvisoryProvenanceNamesTheDecidingRecordForOSClass(t *testing.T) {
	t.Parallel()

	dla := AdvisoryProvenanceObservation{
		Source: "osv", AdvisoryID: "DLA-9999-1", CVEFactID: "cve-dla", AffectedFactID: "pkg-dla",
		releaseTier: osReleaseTierListed,
	}
	debianCVE := AdvisoryProvenanceObservation{
		Source: "osv", AdvisoryID: "DEBIAN-CVE-2023-38039", CVEFactID: "cve-debian", AffectedFactID: "pkg-debian",
		releaseTier: osReleaseTierUnlisted,
	}
	nvd := AdvisoryProvenanceObservation{Source: "nvd", AdvisoryID: "CVE-2023-38039", CVEFactID: "cve-nvd", SeverityScore: 7.5}
	ghsa := AdvisoryProvenanceObservation{Source: "ghsa", AdvisoryID: "GHSA-aaaa-bbbb-cccc", CVEFactID: "cve-ghsa", SeverityScore: 8.1}

	for _, ecosystem := range []string{"os", "OS", "deb", "debian", "ubuntu", "apk", "alpine", "rpm"} {
		tests := []struct {
			name         string
			observations []AdvisoryProvenanceObservation
			wantScore    float64
		}{
			{name: "records only", observations: []AdvisoryProvenanceObservation{dla, debianCVE}},
			{name: "scored nvd", observations: []AdvisoryProvenanceObservation{debianCVE, nvd, dla}, wantScore: 7.5},
			{name: "scored ghsa", observations: []AdvisoryProvenanceObservation{ghsa, dla, debianCVE}, wantScore: 8.1},
			{name: "scored nvd and ghsa", observations: []AdvisoryProvenanceObservation{nvd, ghsa, debianCVE, dla}},
		}
		for _, tc := range tests {
			selection := selectAdvisoryProvenance(ecosystem, tc.observations)
			if selection.DecidingAdvisoryID != "DLA-9999-1" {
				t.Errorf("%s/%s: DecidingAdvisoryID = %q, want DLA-9999-1", ecosystem, tc.name, selection.DecidingAdvisoryID)
			}
			if got := provenanceAdvisoryID(selection, supplyChainCVEGroup{}); got != "DLA-9999-1" {
				t.Errorf("%s/%s: advisory id = %q, want DLA-9999-1", ecosystem, tc.name, got)
			}
			if tc.wantScore != 0 && selection.SeverityScore != tc.wantScore {
				t.Errorf("%s/%s: SeverityScore = %v, want %v", ecosystem, tc.name, selection.SeverityScore, tc.wantScore)
			}
		}
	}
}

// TestSelectAdvisoryProvenanceDecidingRecordRules pins the retraction edges and
// that a non-OS ecosystem never gets a deciding record.
func TestSelectAdvisoryProvenanceDecidingRecordRules(t *testing.T) {
	t.Parallel()

	const withdrawn = "2026-01-02T00:00:00Z"
	liveZ := AdvisoryProvenanceObservation{Source: "osv", AdvisoryID: "Z-1", AffectedFactID: "pz"}
	retractedA := AdvisoryProvenanceObservation{Source: "osv", AdvisoryID: "A-1", AffectedFactID: "pa", WithdrawnAt: withdrawn}
	rejectedB := AdvisoryProvenanceObservation{Source: "osv", AdvisoryID: "B-1", AffectedFactID: "pb", Status: "Rejected"}
	noFact := AdvisoryProvenanceObservation{Source: "osv", AdvisoryID: "A-0"}

	tests := []struct {
		name         string
		ecosystem    string
		observations []AdvisoryProvenanceObservation
		want         string
	}{
		{"a live fact beats a retracted one that sorts first", "os", []AdvisoryProvenanceObservation{retractedA, liveZ}, "Z-1"},
		{"a rejected record does not decide either", "os", []AdvisoryProvenanceObservation{rejectedB, liveZ}, "Z-1"},
		{"wholly retracted: the first retracted record names it", "os", []AdvisoryProvenanceObservation{rejectedB, retractedA}, "A-1"},
		{"an observation with no fact does not decide", "os", []AdvisoryProvenanceObservation{noFact, liveZ}, "Z-1"},
		{"no fact at all leaves it empty", "os", []AdvisoryProvenanceObservation{noFact}, ""},
		{"language ecosystem has no deciding record", "npm", []AdvisoryProvenanceObservation{liveZ}, ""},
		{"unknown ecosystem has no deciding record", "", []AdvisoryProvenanceObservation{liveZ}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := selectAdvisoryProvenance(tc.ecosystem, tc.observations).DecidingAdvisoryID; got != tc.want {
				t.Errorf("DecidingAdvisoryID = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestOSFindingIdentityDoesNotDependOnWhatThePassLoaded: finding_id hashes
// advisory_id, so a name that depends on whether this pass loaded a scored
// record would move the identity of every grouped OS finding between passes.
//
// Measured on the input below (bullseye, DLA plus DEBIAN-CVE): c35a68c258 read
// advisory_id DLA-9999-1 without an NVD record and CVE-2023-38039 with a scored
// one, so the finding_id moved with the load.
func TestOSFindingIdentityDoesNotDependOnWhatThePassLoaded(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	base := append(upstreamEnvelopes(t, nil,
		debianRecord("DEBIAN-CVE-2023-38039", "12", "bookworm", "7.88.1-10+deb12u3", cve),
		debianRecord("DLA-9999-1", "11", "bullseye", "7.74.0-1.3+deb11u10", cve),
	), debianCurl("11", "7.74.0-1.3+deb11u7").envelope())
	nvd := vulnerabilityCVEFactWithProvenance("nvd-1", cve, "nvd", cve, 7.5, "", "HIGH", "2026-10-01T00:00:00Z")
	ghsa := vulnerabilityCVEFactWithProvenance("ghsa-1", cve, "osv", "GHSA-aaaa-bbbb-cccc", 8.1, "", "HIGH", "2026-10-01T00:00:00Z")

	want := BuildSupplyChainImpactFindings(base)
	if len(want) != 1 || want[0].AdvisoryID != "DLA-9999-1" {
		t.Fatalf("baseline = %#v, want one finding named DLA-9999-1", want)
	}
	wantID := supplyChainImpactFindingID(want[0])
	for name, tc := range map[string]struct {
		extra     []facts.Envelope
		wantScore float64
	}{
		"scored nvd":  {extra: []facts.Envelope{nvd}, wantScore: 7.5},
		"scored ghsa": {extra: []facts.Envelope{ghsa}, wantScore: 8.1},
	} {
		for order, shuffled := range orderings(append(slices.Clone(base), tc.extra...)) {
			got := BuildSupplyChainImpactFindings(shuffled)
			if len(got) != 1 {
				t.Fatalf("%s %s: findings = %d, want 1: %#v", name, order, len(got), got)
			}
			if got[0].AdvisoryID != "DLA-9999-1" {
				t.Errorf("%s %s: AdvisoryID = %q, want DLA-9999-1", name, order, got[0].AdvisoryID)
			}
			if id := supplyChainImpactFindingID(got[0]); id != wantID {
				t.Errorf("%s %s: finding_id = %q, want %q: the identity moved with the load", name, order, id, wantID)
			}
			if got[0].CVSSScore != tc.wantScore {
				t.Errorf("%s %s: CVSSScore = %v, want %v: the scored record still supplies the score", name, order, got[0].CVSSScore, tc.wantScore)
			}
		}
	}
}

// TestRetractedRecordDoesNotNameALiveGroup completes
// TestRetractedRecordDoesNotDecideALiveGroup: the live record names the finding,
// and restoring a single record's retraction does not move its name (#9695).
func TestRetractedRecordDoesNotNameALiveGroup(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	host := debianCurl("11", "7.74.0-1.3+deb11u7").envelope()
	for name, status := range map[string]string{"withdrawn": "", "rejected": "Rejected"} {
		dla, _ := retractedDLA(t, status)
		mixed := append(upstreamEnvelopes(t, nil,
			debianRecord("DEBIAN-CVE-2023-38039", "12", "bookworm", "7.88.1-10+deb12u3", cve)), dla...)
		got := BuildSupplyChainImpactFindings(append(mixed, host))
		if len(got) != 1 || got[0].AdvisoryID != "DEBIAN-CVE-2023-38039" {
			t.Errorf("%s mixed: findings = %#v, want one named DEBIAN-CVE-2023-38039", name, got)
		}
		alone := BuildSupplyChainImpactFindings(append(slices.Clone(dla), host))
		if len(alone) != 1 || alone[0].AdvisoryID != "DLA-9999-1" {
			t.Errorf("%s alone: findings = %#v, want one named DLA-9999-1", name, alone)
		}
	}
}

// TestLanguageFindingIdentityKeepsTheScoredSourceRule is the #9693 control: a
// non-OS finding is still named by the source-reported score. The expected ids
// match base e3d816172a byte for byte.
func TestLanguageFindingIdentityKeepsTheScoredSourceRule(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2021-23337"
	consumption := func(packageID string) facts.Envelope {
		return packageConsumptionFactWithRange("consumption-1", packageID, testImpactRepositoryID, "4.17.20")
	}
	tests := []struct {
		name string
		cves []facts.Envelope
		want string
	}{
		{
			name: "scored ghsa beside scored nvd names the ghsa",
			cves: []facts.Envelope{
				vulnerabilityCVEFactWithProvenance("ghsa-cve", cve, "osv", "GHSA-35jh-r3h4-6jhm", 7.2, "", "HIGH", "2026-10-01T00:00:00Z"),
				vulnerabilityCVEFactWithProvenance("nvd-cve", cve, "nvd", cve, 7.5, "", "HIGH", "2026-10-01T00:00:00Z"),
			},
			want: "GHSA-35jh-r3h4-6jhm",
		},
		{
			name: "unscored ghsa beside scored nvd names the nvd record",
			cves: []facts.Envelope{
				vulnerabilityCVEFactWithProvenance("ghsa-cve", cve, "osv", "GHSA-35jh-r3h4-6jhm", 0, "", "", "2026-10-01T00:00:00Z"),
				vulnerabilityCVEFactWithProvenance("nvd-cve", cve, "nvd", cve, 7.5, "", "HIGH", "2026-10-01T00:00:00Z"),
			},
			want: cve,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			packageID := "npm://registry.npmjs.org/lodash"
			affected := vulnerabilityAffectedPackageFact("ghsa-pkg", cve, packageID, "npm", "lodash", "4.17.20", "4.17.21")
			affected.Payload["advisory_id"] = "GHSA-35jh-r3h4-6jhm"
			affected.Payload["source"] = "osv"
			envelopes := append(slices.Clone(tc.cves), affected, consumption(packageID))
			got := BuildSupplyChainImpactFindings(envelopes)
			if len(got) != 1 || got[0].AdvisoryID != tc.want {
				t.Fatalf("findings = %#v, want one named %q", got, tc.want)
			}
		})
	}
}
