// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
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

// TestUpstreamGroupingSiblingFactKeepsItsOwnCVE: a second cve fact of a grouped
// record, keyed to a CVE its upstream list does not name, keeps that CVE's group.
// The stored record says it covers that CVE; the newer list adding another one is
// no reason to drop it, and which fact arrives first must not decide.
func TestUpstreamGroupingSiblingFactKeepsItsOwnCVE(t *testing.T) {
	t.Parallel()

	const named, sibling = "CVE-2023-38039", "CVE-2023-38040"
	envelopes := convertedOSVRecord(t, debianRecord("DSA-9998-1", "12", "bookworm", "7.88.1-10+deb12u3", named), false)
	for _, envelope := range convertedOSVRecord(t, debianRecord("DSA-9998-1", "12", "bookworm", "7.88.1-10+deb12u3"), false) {
		if envelope.FactKind == chain.VulnerabilityCVEFactKind {
			envelope.FactID += "-sibling"
			envelope.Payload["cve_id"] = sibling
			envelopes = append(envelopes, envelope)
		}
	}
	envelopes = append(envelopes, debianCurl("12", "7.88.1-10+deb12u2").envelope())
	for order, shuffled := range orderings(envelopes) {
		got := map[string]string{}
		for _, finding := range BuildSupplyChainImpactFindings(shuffled) {
			got[finding.CVEID] = finding.AdvisoryID
		}
		if want := map[string]string{named: "DSA-9998-1", sibling: "DSA-9998-1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: advisory by CVE = %v, want %v", order, got, want)
		}
	}
}

// TestUpstreamGroupingCopiesOnlyArrivedFacts: every copy comes from a fact that
// arrived, never from an earlier copy. A record with two cve facts is the case
// where a copy lands in a bucket the pass has yet to read: reading it would file
// the package twice under the record's other CVE. The classifier folds the
// duplicate, so this reads the index.
func TestUpstreamGroupingCopiesOnlyArrivedFacts(t *testing.T) {
	t.Parallel()

	const id, b, c = "DSA-9998-1", "CVE-2026-0002", "CVE-2026-0003"
	index := supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{FactID: "cve-1", CVEID: id, AdvisoryID: id, Upstream: []string{b, c}},
			{FactID: "cve-2", CVEID: b, AdvisoryID: id},
		},
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{
			id: {{FactID: "pkg-1", CVEID: id, AdvisoryID: id, PackageID: "os://debian/curl"}},
		},
	}
	groupOSAdvisoriesByUpstream(&index)
	for _, key := range []string{b, c} {
		if got := packageFactIDs(index.affectedPackages[key]); !reflect.DeepEqual(got, []string{"pkg-1"}) {
			t.Errorf("packages under %s = %v, want pkg-1 once", key, got)
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

// TestUpstreamGroupingIsNotTransitive: A names B and B names C, each with its own
// package. A's package reads under its own keys only: reaching C through B would
// file a package under a CVE its record never named.
func TestUpstreamGroupingIsNotTransitive(t *testing.T) {
	t.Parallel()

	chained := func(id, name, alias string, upstream ...string) vulnerabilityintelligence.OSVVulnerability {
		record := distroRecord(id, "Debian:12", "pkg:deb/debian/"+name+"?arch=source&distro=bookworm", "7.88.1-10+deb12u3", upstream)
		record.Affected[0].Package.Name = name
		record.Aliases = []string{alias}
		return record
	}
	host := func(name string) facts.Envelope {
		row := debianCurl("12", "7.88.1-10+deb12u2")
		row.name = name
		return row.envelope()
	}
	envelopes := upstreamEnvelopes(t, nil,
		chained("DSA-1-1", "aaa", "CVE-2026-0001", "CVE-2026-0002"),
		chained("DSA-2-1", "bbb", "CVE-2026-0002", "CVE-2026-0003"),
		chained("DSA-3-1", "ccc", "CVE-2026-0003"),
	)
	envelopes = append(envelopes, host("aaa"), host("bbb"), host("ccc"))

	want := map[string][]string{
		"CVE-2026-0001": {"os://debian/aaa"},
		"CVE-2026-0002": {"os://debian/aaa", "os://debian/bbb"},
		"CVE-2026-0003": {"os://debian/bbb", "os://debian/ccc"},
	}
	for order, shuffled := range orderings(envelopes) {
		got := map[string][]string{}
		for _, finding := range BuildSupplyChainImpactFindings(shuffled) {
			got[finding.CVEID] = append(got[finding.CVEID], finding.PackageID)
		}
		for key := range got {
			slices.Sort(got[key])
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: packages by CVE = %v, want %v", order, got, want)
		}
	}
}

// TestUpstreamGroupingBundleBesideAPerCVERecord is decision 14 (vi) of the #9914
// ruling: a USN bundle and the UBUNTU-CVE record for one of its CVEs read one
// finding for that CVE, not two, and the bundle's other CVE still reads its own.
// Both records hold the host's version, so the higher fix names the shared CVE, and
// equal fixes keep sort order. Real USN and UBUNTU-CVE pairs read in review agree on
// the fix (USN-5412-1 and UBUNTU-CVE-2022-27780, both 7.81.0-1ubuntu1.2).
func TestUpstreamGroupingBundleBesideAPerCVERecord(t *testing.T) {
	t.Parallel()

	const purl = "pkg:deb/ubuntu/curl?arch=source&distro=jammy"
	for name, tc := range map[string]struct {
		perCVEFix string
		wantNamed string
	}{
		"equal fixes keep sort order": {"7.81.0-1ubuntu1.17", "UBUNTU-CVE-2024-2398"},
		"the bundle's higher fix":     {"7.81.0-1ubuntu1.16", "USN-6766-1"},
		"the per-CVE record's higher": {"7.81.0-1ubuntu1.18", "UBUNTU-CVE-2024-2398"},
	} {
		envelopes := upstreamEnvelopes(t, nil,
			distroRecord("USN-6766-1", "Ubuntu:22.04:LTS", purl, "7.81.0-1ubuntu1.17",
				[]string{"CVE-2024-2398", "CVE-2024-2004", "UBUNTU-CVE-2024-2398"}),
			distroRecord("UBUNTU-CVE-2024-2398", "Ubuntu:22.04:LTS", purl, tc.perCVEFix, []string{"CVE-2024-2398"}),
		)
		envelopes = append(envelopes, ubuntuCurl("22.04", "7.81.0-1ubuntu1.1").envelope())

		want := map[string]string{"CVE-2024-2004": "USN-6766-1", "CVE-2024-2398": tc.wantNamed}
		for order, shuffled := range orderings(envelopes) {
			got := map[string]string{}
			findings := BuildSupplyChainImpactFindings(shuffled)
			for _, finding := range findings {
				got[finding.CVEID] = finding.AdvisoryID
				assertSupplyChainImpactStatus(t, finding, SupplyChainImpactAffectedExact)
			}
			if len(findings) != 2 || !reflect.DeepEqual(got, want) {
				t.Errorf("%s/%s: %d findings, advisory by CVE = %v, want 2 with %v", name, order, len(findings), got, want)
			}
		}
	}
}

func packageFactIDs(pkgs []supplychainmodel.AffectedPackage) []string {
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		out = append(out, pkg.FactID)
	}
	return out
}
