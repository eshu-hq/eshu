// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// orderedAdvisoryGroup is one CVE group's records for the provenance ordering
// test: the model-level view of what the grouping pass hands the classifier
// after selectOSAdvisoryRelease narrowed each OSV copy to the host's line.
type orderedAdvisoryGroup struct {
	cves []supplychainmodel.ImpactCVE
	pkgs []supplychainmodel.AffectedPackage
}

func (g orderedAdvisoryGroup) add(source, advisoryID string, pkg supplychainmodel.AffectedPackage) orderedAdvisoryGroup {
	pkg.CVEID, pkg.AdvisoryID, pkg.Source, pkg.FactID = "CVE-2099-9914", advisoryID, source, "affected-"+advisoryID
	g.pkgs = append(g.pkgs, pkg)
	g.cves = append(g.cves, supplychainmodel.ImpactCVE{
		FactID: "cve-" + advisoryID, CVEID: "CVE-2099-9914", AdvisoryID: advisoryID, Source: source,
	})
	return g
}

// selection runs the production path the classifier takes for a group:
// observations from the cve and affected facts, then provenance selection.
func (g orderedAdvisoryGroup) selection() (advisoryProvenanceSelection, string) {
	observations := buildAdvisoryProvenanceObservations(g.cves, g.pkgs)
	selection := selectAdvisoryProvenance("os", observations)
	return selection, provenanceAdvisoryID(selection, supplyChainCVEGroup{cveID: "CVE-2099-9914", observations: g.cves})
}

// TestAdvisoryProvenanceReleaseTierOrdersSameSourceRecords pins decision 8 of
// the #9914 ruling: within one source, a record whose fact lists the host's
// release line outranks an untagged one (a vendor-sourced fact in every
// verification state, which carries no line the join checks), which outranks
// one that lists other lines only. advisory_id names the record that decided
// and the fix, range and source come from it; the AdvisoryID string order only
// breaks ties inside a tier.
func TestAdvisoryProvenanceReleaseTierOrdersSameSourceRecords(t *testing.T) {
	t.Parallel()

	rangeEvents := func(fixed string, releases ...string) []supplychainmodel.AffectedRange {
		return []supplychainmodel.AffectedRange{{
			Kind:     "ECOSYSTEM",
			Events:   []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: fixed}},
			Releases: releases,
		}}
	}
	// DEBIAN-CVE sorts before DLA before DSA by id, so any id-ordered fallback
	// would pick the unlisted copy: the tier must override it.
	unlisted := supplychainmodel.AffectedPackage{
		PackageID: "os://debian/curl", Ecosystem: "os", PURL: "pkg:deb/debian/curl?arch=source&distro=bookworm",
		AffectedRanges: rangeEvents("7.88.1-10+deb12u3", "debian:12"), ReleaseUnlisted: true,
	}
	listed := supplychainmodel.AffectedPackage{
		PackageID: "os://debian/curl", Ecosystem: "os", PURL: "pkg:deb/debian/curl?arch=source&distro=bullseye",
		AffectedRanges: rangeEvents("7.74.0-1.3+deb11u10", "debian:11"), FixedVersions: []string{"7.74.0-1.3+deb11u10"},
		ReleaseLine: "debian:11",
	}
	// Vendor-sourced facts: verified (purl names the host's line), unknown (no
	// line in the purl), and mismatch (purl names another line). None of them
	// is narrowed, so all rank in the untagged tier.
	vendorFacts := map[string]supplychainmodel.AffectedPackage{}
	for name, purl := range map[string]string{
		"verified": "pkg:deb/debian/curl?arch=source&distro=debian-11",
		"unknown":  "pkg:deb/debian/curl?arch=source",
		"mismatch": "pkg:deb/debian/curl?arch=source&distro=debian-12",
	} {
		vendorFacts[name] = supplychainmodel.AffectedPackage{
			PackageID: "os://debian/curl", Ecosystem: "os", PURL: purl,
			AffectedRanges: rangeEvents("7.74.0-1.3+deb11u8"), FixedVersions: []string{"7.74.0-1.3+deb11u8"},
		}
	}

	for name, vendor := range vendorFacts {
		build := func(parts ...string) orderedAdvisoryGroup {
			var g orderedAdvisoryGroup
			for _, part := range parts {
				switch part {
				case "unlisted":
					g = g.add("osv", "DEBIAN-CVE-2099-9914", unlisted)
				case "listed":
					g = g.add("osv", "DLA-9999-1", listed)
				case "vendor":
					g = g.add("debian", "DSA-9998-1", vendor)
				}
			}
			return g
		}
		tests := []struct {
			parts        []string
			wantAdvisory string
			wantRange    string
			wantFix      string
		}{
			{[]string{"unlisted", "listed", "vendor"}, "DLA-9999-1", supplyChainAffectedRangeSummary(listed), "7.74.0-1.3+deb11u10"},
			{[]string{"unlisted", "vendor"}, "DSA-9998-1", supplyChainAffectedRangeSummary(vendor), "7.74.0-1.3+deb11u8"},
			{[]string{"unlisted", "listed"}, "DLA-9999-1", supplyChainAffectedRangeSummary(listed), "7.74.0-1.3+deb11u10"},
			{[]string{"unlisted"}, "DEBIAN-CVE-2099-9914", supplyChainAffectedRangeSummary(unlisted), ""},
		}
		for _, tc := range tests {
			group := build(tc.parts...)
			// Every permutation class: given, reversed, three seeded shuffles.
			for run := 0; run < 5; run++ {
				cves, pkgs := slices.Clone(group.cves), slices.Clone(group.pkgs)
				switch run {
				case 1:
					slices.Reverse(cves)
					slices.Reverse(pkgs)
				case 2, 3, 4:
					rng := rand.New(rand.NewSource(int64(run)))
					rng.Shuffle(len(cves), func(i, j int) { cves[i], cves[j] = cves[j], cves[i] })
					rng.Shuffle(len(pkgs), func(i, j int) { pkgs[i], pkgs[j] = pkgs[j], pkgs[i] })
				}
				selection, advisoryID := orderedAdvisoryGroup{cves: cves, pkgs: pkgs}.selection()
				label := fmt.Sprintf("%s vendor fact, %v, run %d", name, tc.parts, run)
				if advisoryID != tc.wantAdvisory {
					t.Errorf("%s: advisory_id = %q, want %q", label, advisoryID, tc.wantAdvisory)
				}
				if selection.VulnerableRange != tc.wantRange {
					t.Errorf("%s: VulnerableRange = %q, want %q", label, selection.VulnerableRange, tc.wantRange)
				}
				if selection.FixedVersion != tc.wantFix {
					t.Errorf("%s: FixedVersion = %q, want %q", label, selection.FixedVersion, tc.wantFix)
				}
			}
		}
	}
}

// TestAdvisoryProvenanceTierIsZeroWeightWithoutReleaseNarrowing: observations
// from facts that were never narrowed keep the order they had before the tier
// existed, so no non-OS group and no OS group without release tags moves.
func TestAdvisoryProvenanceTierIsZeroWeightWithoutReleaseNarrowing(t *testing.T) {
	t.Parallel()

	var g orderedAdvisoryGroup
	for _, id := range []string{"DSA-3", "DLA-2", "DEBIAN-CVE-1"} {
		g = g.add("osv", id, supplychainmodel.AffectedPackage{
			PackageID: "os://debian/curl", Ecosystem: "os", PURL: "pkg:deb/debian/curl",
		})
	}
	selection, advisoryID := g.selection()
	if advisoryID != "DEBIAN-CVE-1" {
		t.Fatalf("advisory_id = %q, want the AdvisoryID order DEBIAN-CVE-1 first when no fact is narrowed", advisoryID)
	}
	var ids []string
	for _, source := range selection.AdvisorySources {
		ids = append(ids, source.AdvisoryID)
	}
	if want := []string{"DEBIAN-CVE-1", "DLA-2", "DSA-3"}; !slices.Equal(ids, want) {
		t.Fatalf("advisory sources = %v, want %v", ids, want)
	}
}
