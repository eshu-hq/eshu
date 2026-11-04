// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestUpstreamGroupingJoinsOnAdvisoryIDNotCVEID: a record whose alias gives it
// the same cve_id as an unrelated GHSA record must not drag that GHSA's
// packages to the other CVEs its upstream list names.
//
// Measured on the input below: base e3d816172a reads 1 finding (the lodash-rails
// one under CVE-2021-23337); c35a68c258, which joined on cve_id, read 3, adding
// CVE-2026-2020 and CVE-2026-2021 as affected_exact on a gem they have nothing
// to do with.
func TestUpstreamGroupingJoinsOnAdvisoryIDNotCVEID(t *testing.T) {
	t.Parallel()

	ghsa := convertedOSVFixture(t, "ghsa-35jh-r3h4-6jhm.json", false)
	var packageID string
	for _, envelope := range ghsa {
		if envelope.FactKind == chain.VulnerabilityAffectedPackageFactKind {
			packageID, _ = envelope.Payload["package_id"].(string)
		}
	}
	if packageID == "" {
		t.Fatal("fixture carries no affected package")
	}
	consumption := packageConsumptionFactWithRange("consumption-lodash", packageID, testImpactRepositoryID, "4.17.20")

	dsa := debianRecord("DSA-9002-1", "12", "bookworm", "7.88.1-10+deb12u3", "CVE-2026-2020", "CVE-2026-2021")
	dsa.Aliases = []string{"CVE-2021-23337"}

	baseline := BuildSupplyChainImpactFindings(append(slices.Clone(ghsa), consumption))
	if len(baseline) != 1 || baseline[0].CVEID != "CVE-2021-23337" {
		t.Fatalf("baseline = %#v, want the one lodash finding", baseline)
	}
	envelopes := append(append(slices.Clone(ghsa), consumption), convertedOSVRecord(t, dsa, false)...)
	for order, shuffled := range orderings(envelopes) {
		got := BuildSupplyChainImpactFindings(shuffled)
		if len(got) != 1 {
			t.Fatalf("%s: findings = %d, want 1: %#v", order, len(got), got)
		}
		if got[0].CVEID != "CVE-2021-23337" || got[0].PackageID != packageID || got[0].AdvisoryID != baseline[0].AdvisoryID {
			t.Errorf("%s: cve/package/advisory = %q/%q/%q, want the baseline %q/%q/%q", order,
				got[0].CVEID, got[0].PackageID, got[0].AdvisoryID, baseline[0].CVEID, baseline[0].PackageID, baseline[0].AdvisoryID)
		}
	}
}

// TestUpstreamGroupingAliasRecordFilesUnderEveryKey is the DSA-9002-1 golden
// shape: cve_id comes from an alias and upstream names two other CVEs, so the
// record reads N+1 findings for one installed package, and each carries the
// record's own two fact ids.
func TestUpstreamGroupingAliasRecordFilesUnderEveryKey(t *testing.T) {
	t.Parallel()

	dsa := debianRecord("DSA-9002-1", "12", "bookworm", "7.88.1-10+deb12u3", "CVE-2026-2020", "CVE-2026-2021")
	dsa.Aliases = []string{"CVE-2026-2010"}
	record := convertedOSVRecord(t, dsa, false)
	var recordFacts []string
	for _, envelope := range record {
		if envelope.FactKind == chain.VulnerabilityCVEFactKind || envelope.FactKind == chain.VulnerabilityAffectedPackageFactKind {
			recordFacts = append(recordFacts, envelope.FactID)
		}
	}
	if len(recordFacts) != 2 {
		t.Fatalf("record facts = %v, want one cve and one affected_package fact", recordFacts)
	}
	envelopes := append(slices.Clone(record), debianCurl("12", "7.88.1-10+deb12u2").envelope())
	for order, shuffled := range orderings(envelopes) {
		findings := BuildSupplyChainImpactFindings(shuffled)
		var cves []string
		for _, finding := range findings {
			cves = append(cves, finding.CVEID)
			if finding.AdvisoryID != "DSA-9002-1" {
				t.Errorf("%s: %s AdvisoryID = %q, want DSA-9002-1", order, finding.CVEID, finding.AdvisoryID)
			}
			for _, id := range recordFacts {
				if !slices.Contains(finding.EvidenceFactIDs, id) {
					t.Errorf("%s: %s evidence %v is missing the record's fact %q", order, finding.CVEID, finding.EvidenceFactIDs, id)
				}
			}
		}
		slices.Sort(cves)
		if want := []string{"CVE-2026-2010", "CVE-2026-2020", "CVE-2026-2021"}; !slices.Equal(cves, want) {
			t.Fatalf("%s: finding CVEs = %v, want %v", order, cves, want)
		}
	}
}

// TestUpstreamGroupingLeavesOtherRecordsPackagesUnderASharedKey: only the
// grouped record's packages leave a key it does not keep. Another record filed
// under the same cve_id stays there.
func TestUpstreamGroupingLeavesOtherRecordsPackagesUnderASharedKey(t *testing.T) {
	t.Parallel()

	index := supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{FactID: "a", CVEID: "SHARED-1", AdvisoryID: "DSA-1", Upstream: []string{"CVE-2026-1"}},
			{FactID: "b", CVEID: "SHARED-1", AdvisoryID: "DLA-1"},
		},
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{
			"SHARED-1": {
				{FactID: "pa", CVEID: "SHARED-1", AdvisoryID: "DSA-1"},
				{FactID: "pb", CVEID: "SHARED-1", AdvisoryID: "DLA-1"},
			},
		},
	}
	groupOSAdvisoriesByUpstream(&index)

	if got := packageFactIDs(index.affectedPackages["SHARED-1"]); !slices.Equal(got, []string{"pb"}) {
		t.Errorf("SHARED-1 packages = %v, want the unrelated record's [pb] only", got)
	}
	if got := packageFactIDs(index.affectedPackages["CVE-2026-1"]); !slices.Equal(got, []string{"pa"}) {
		t.Errorf("CVE-2026-1 packages = %v, want the grouped record's [pa] only", got)
	}
}

// TestUpstreamGroupingTwoCVEFactsOfOneAdvisoryAreOneRecord: a second cve fact
// of the record that carries no upstream list follows the record to its keys,
// so its fact id stays on the finding and no group is left behind.
func TestUpstreamGroupingTwoCVEFactsOfOneAdvisoryAreOneRecord(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	with := convertedOSVRecord(t, debianRecord("DSA-9998-1", "12", "bookworm", "7.88.1-10+deb12u3", cve), false)
	without := convertedOSVRecord(t, debianRecord("DSA-9998-1", "12", "bookworm", "7.88.1-10+deb12u3"), false)
	var older facts.Envelope
	for _, envelope := range without {
		if envelope.FactKind == chain.VulnerabilityCVEFactKind {
			older = envelope
			older.FactID = envelope.FactID + "-older"
		}
	}
	envelopes := append(append(slices.Clone(with), older), debianCurl("12", "7.88.1-10+deb12u2").envelope())
	for order, shuffled := range orderings(envelopes) {
		findings := BuildSupplyChainImpactFindings(shuffled)
		if len(findings) != 1 || findings[0].CVEID != cve {
			t.Fatalf("%s: findings = %#v, want one finding under %s", order, findings, cve)
		}
		if !slices.Contains(findings[0].EvidenceFactIDs, older.FactID) {
			t.Errorf("%s: evidence %v is missing the second cve fact %q", order, findings[0].EvidenceFactIDs, older.FactID)
		}
	}
}

// TestUpstreamGroupingBlankAdvisoryIDFailsClosed: a record with no advisory id
// cannot be joined to its packages, so it stays under its own key and counts as
// ungrouped. Production envelopes always carry one and the typed decode requires
// it, so this builds the index directly.
func TestUpstreamGroupingBlankAdvisoryIDFailsClosed(t *testing.T) {
	t.Parallel()

	build := func(upstream []string) supplyChainImpactIndex {
		index := supplyChainImpactIndex{
			cves: []supplychainmodel.ImpactCVE{{FactID: "c", CVEID: "DEBIAN-CVE-2026-1", Upstream: upstream}},
			affectedPackages: map[string][]supplychainmodel.AffectedPackage{
				"DEBIAN-CVE-2026-1": {{FactID: "p", CVEID: "DEBIAN-CVE-2026-1"}},
			},
		}
		groupOSAdvisoriesByUpstream(&index)
		return index
	}
	with, without := build([]string{"CVE-2026-1"}), build(nil)

	if len(with.cves) != 1 || with.cves[0].CVEID != "DEBIAN-CVE-2026-1" {
		t.Errorf("cves = %#v, want the record under its own key", with.cves)
	}
	if !reflect.DeepEqual(with.affectedPackages, without.affectedPackages) {
		t.Errorf("packages = %#v, want the no-upstream result %#v", with.affectedPackages, without.affectedPackages)
	}
	if with.osUpstream.rekeyed {
		t.Error("rekeyed = true, want false: nothing was copied")
	}
	want := map[osUpstreamGroupKey]int{{source: "debian", result: osUpstreamUngrouped}: 1}
	if !reflect.DeepEqual(with.osUpstream.cells, want) {
		t.Errorf("cells = %#v, want %#v", with.osUpstream.cells, want)
	}
}

func packageFactIDs(pkgs []supplychainmodel.AffectedPackage) []string {
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		out = append(out, pkg.FactID)
	}
	return out
}
