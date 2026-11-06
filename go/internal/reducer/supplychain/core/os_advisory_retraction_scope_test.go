// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestOSRetractionIsJudgedOverTheRecordsThatListThePackage: a withdrawn or
// rejected record that is the only record listing curl names and decides the
// finding, so the finding must say so. A live record that lists another package,
// or a live cve-only record, never asserted that curl is affected and cannot keep
// the withdrawn assertion alive.
//
// Measured on the first input (host Debian 11 curl, withdrawn DLA-9999-1, live
// DEBIAN-CVE listing libfoo): base e3d816172a read affected_exact with
// AdvisoryRetraction{debian withdrawn DLA-9999-1}. 9c99579a2a read affected_exact,
// no fix, no retraction, and with a scored NVD beside it cvss 9.8, no retraction.
func TestOSRetractionIsJudgedOverTheRecordsThatListThePackage(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	libfooKey := "built-retraction-libfoo"
	scoredNVD := vulnerabilityCVEFactWithProvenance("nvd-1", cve, "nvd", cve, 9.8, "", "CRITICAL", "2026-10-01T00:00:00Z")

	variants := map[string]struct {
		live      []facts.Envelope
		wantScore float64
	}{
		"live record listing another package": {live: upstreamEnvelopes(t, nil, libfooKey)},
		"live scored nvd":                     {live: []facts.Envelope{scoredNVD}, wantScore: 9.8},
		"both":                                {live: append(upstreamEnvelopes(t, nil, libfooKey), scoredNVD), wantScore: 9.8},
	}
	for name, variant := range variants {
		for status, wantCode := range map[string]string{"": AdvisoryRetractionWithdrawn, "Rejected": AdvisoryRetractionRejected} {
			dla, dlaFacts := retractedDLA(t, status)
			envelopes := append(slices.Clone(variant.live), dla...)
			envelopes = append(envelopes, debianCurl("11", "7.74.0-1.3+deb11u7").envelope())
			for order, shuffled := range orderings(envelopes) {
				findings := BuildSupplyChainImpactFindings(shuffled)
				if len(findings) != 1 {
					t.Fatalf("%s/%s: findings = %d, want 1: %#v", name, order, len(findings), findings)
				}
				got := findings[0]
				assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
				if got.AdvisoryID != "DLA-9999-1" {
					t.Errorf("%s/%s: AdvisoryID = %q, want DLA-9999-1", name, order, got.AdvisoryID)
				}
				if got.AdvisoryRetraction == nil {
					t.Errorf("%s/%s/%q: AdvisoryRetraction = nil, want the DLA's retraction", name, order, status)
					continue
				}
				if sources := got.AdvisoryRetraction.Sources; len(sources) != 1 ||
					sources[0].AdvisoryID != "DLA-9999-1" || sources[0].Code != wantCode {
					t.Errorf("%s/%s/%q: retraction sources = %#v, want only the DLA with code %q", name, order, status, sources, wantCode)
				}
				if got.CVSSScore != variant.wantScore {
					t.Errorf("%s/%s: CVSSScore = %v, want %v: a retraction does not drop the score", name, order, got.CVSSScore, variant.wantScore)
				}
				if got.FixedVersion != "" {
					t.Errorf("%s/%s: FixedVersion = %q, want none: a retracted record's fix is not offered", name, order, got.FixedVersion)
				}
				if !slices.Contains(got.EvidenceFactIDs, dlaFacts[0]) {
					t.Errorf("%s/%s: evidence %v is missing the DLA's fact", name, order, got.EvidenceFactIDs)
				}
			}
		}
	}
}

// TestOSRetractionKeepsARecordWithOneLiveCVEFactLive: two cve facts sharing an
// AdvisoryID are one record, whichever of them the package fact pairs with. Live
// beside withdrawn stays live, as liveAdvisoryPackages reads it, so the retraction
// cannot depend on the order the facts load in.
func TestOSRetractionKeepsARecordWithOneLiveCVEFactLive(t *testing.T) {
	t.Parallel()

	dla, _ := retractedDLA(t, "")
	envelopes := append(slices.Clone(dla),
		vulnerabilityCVEFactWithProvenance("dla-live-copy", "CVE-2023-38039", "osv", "DLA-9999-1", 0, "", "", "2026-10-01T00:00:00Z"),
		debianCurl("11", "7.74.0-1.3+deb11u7").envelope())
	for order, shuffled := range orderings(envelopes) {
		findings := BuildSupplyChainImpactFindings(shuffled)
		if len(findings) != 1 {
			t.Fatalf("%s: findings = %d, want 1: %#v", order, len(findings), findings)
		}
		if findings[0].AdvisoryRetraction != nil {
			t.Errorf("%s: AdvisoryRetraction = %#v, want nil: one cve fact of the record is live", order, findings[0].AdvisoryRetraction)
		}
	}
}

// TestLanguageRetractionKeepsTheGroupRule is the control: outside the OS classes a
// withdrawn GHSA beside a live NVD record stays unretracted, as on the base. The
// asymmetry is known and out of scope for #9914.
func TestLanguageRetractionKeepsTheGroupRule(t *testing.T) {
	t.Parallel()

	const (
		cve       = "CVE-2021-23337"
		packageID = "npm://registry.npmjs.org/lodash"
	)
	withdrawnGHSA := vulnerabilityCVEFactWithProvenance("ghsa-cve", cve, "osv", "GHSA-35jh-r3h4-6jhm", 7.2, "", "HIGH", "2026-10-01T00:00:00Z")
	withdrawnGHSA.Payload["withdrawn_at"] = "2026-01-02T00:00:00Z"
	nvd := vulnerabilityCVEFactWithProvenance("nvd-cve", cve, "nvd", cve, 7.5, "", "HIGH", "2026-10-01T00:00:00Z")
	affected := vulnerabilityAffectedPackageFact("ghsa-pkg", cve, packageID, "npm", "lodash", "4.17.20", "4.17.21")
	affected.Payload["advisory_id"] = "GHSA-35jh-r3h4-6jhm"
	affected.Payload["source"] = "osv"
	consumption := packageConsumptionFactWithRange("consumption-1", packageID, testImpactRepositoryID, "4.17.20")

	for order, shuffled := range orderings([]facts.Envelope{withdrawnGHSA, nvd, affected, consumption}) {
		findings := BuildSupplyChainImpactFindings(shuffled)
		if len(findings) != 1 {
			t.Fatalf("%s: findings = %d, want 1: %#v", order, len(findings), findings)
		}
		if findings[0].AdvisoryRetraction != nil {
			t.Errorf("%s: AdvisoryRetraction = %#v, want nil: the live NVD record keeps a language finding unretracted", order, findings[0].AdvisoryRetraction)
		}
		if findings[0].AdvisoryID != cve {
			t.Errorf("%s: AdvisoryID = %q, want %q", order, findings[0].AdvisoryID, cve)
		}
	}
}

// TestRecordsListingThePackageDoesNotAllocateWithoutARetraction pins the hot path:
// this runs once per finding, and almost no finding has a retracted record.
func TestRecordsListingThePackageDoesNotAllocateWithoutARetraction(t *testing.T) {
	observations := []AdvisoryProvenanceObservation{{AdvisoryID: "A", AffectedFactID: "pa"}, {AdvisoryID: "CVE-1"}}
	if allocs := testing.AllocsPerRun(100, func() { recordsListingThePackage(observations) }); allocs != 0 {
		t.Errorf("allocs = %v, want 0", allocs)
	}
}
