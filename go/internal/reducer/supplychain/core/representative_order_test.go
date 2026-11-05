// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestJoinableRepresentativeChoiceIgnoresFactOrder pins the choice among several
// facts that join: rank under the vendor of the row each joins, then source, then
// fact id, whichever order the group holds them in. The input slice is left alone:
// the index shares it across scan targets, and another target may join another fact.
func TestJoinableRepresentativeChoiceIgnoresFactOrder(t *testing.T) {
	t.Parallel()
	index := osPackageRowForTest(t)
	fact := func(factID, source, ecosystem, advisoryID string) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{
			FactID: factID, Source: source, Ecosystem: ecosystem, AdvisoryID: advisoryID,
			PackageID: repJoinPackageID, PURL: repJoinPURL, Name: "curl",
		}
	}
	// The source-order winner under deb: a debian fact that cannot join an Ubuntu row.
	loser := fact("a-loser", "debian", "deb", "DSA-1")
	for _, tc := range []struct {
		name  string
		facts []supplychainmodel.AffectedPackage
		want  string
	}{
		{"the row's vendor outranks OSV", []supplychainmodel.AffectedPackage{
			loser, fact("z-osv", "osv", "os", "OSV-1"), fact("b-ubuntu", "ubuntu", "deb", "USN-1"),
		}, "b-ubuntu"},
		{"source breaks a rank tie", []supplychainmodel.AffectedPackage{
			loser, fact("b-ubuntu", "ubuntu", "deb", "USN-1"), fact("z-upper", "UBUNTU", "deb", "USN-2"),
		}, "z-upper"},
		{"fact id breaks a source tie", []supplychainmodel.AffectedPackage{
			loser, fact("c-ubuntu", "ubuntu", "deb", "USN-1"), fact("b-ubuntu", "ubuntu", "deb", "USN-2"),
		}, "b-ubuntu"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, reversed := range []bool{false, true} {
				pkgs := slices.Clone(tc.facts)
				if reversed {
					slices.Reverse(pkgs)
				}
				before := slices.Clone(pkgs)
				marked, _, ok := withJoinableOSRepresentative(pkgs, loser, index)
				if !ok {
					t.Fatal("no fact was marked")
				}
				if !reflect.DeepEqual(pkgs, before) {
					t.Fatalf("the shared input slice was modified: %+v", pkgs)
				}
				if got := representativeAffectedPackage(marked).FactID; got != tc.want {
					t.Fatalf("representative = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// TestJoinableRepresentativeRanksUnderEveryRowVendor pins the vendor-first choice
// on every vendor the grid holds, rpm included: alma and rocky have no entry of their
// own in the ecosystem tables, so ranking through an ecosystem name would put OSV
// ahead of the vendor's fact there.
func TestJoinableRepresentativeRanksUnderEveryRowVendor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		vendor    string
		row       installedOSRow
		packageID string
		query     string
	}{
		{"ubuntu", ubuntuCurl("22.04", repJoinVersion), "pkg:deb/ubuntu/curl", "?distro=ubuntu-22.04"},
		{"debian", debianCurl("12", repJoinVersion), "pkg:deb/debian/curl", "?distro=debian-12"},
		{"alpine", alpineCurl("3.18.4", repJoinVersion), "pkg:apk/alpine/curl", "?distro=alpine-3.18"},
		{"alma", almaCurl("8", repJoinVersion), "pkg:rpm/alma/curl", ""},
		{"rocky", rockyCurl("8", repJoinVersion), "pkg:rpm/rocky/curl", ""},
	} {
		t.Run(tc.vendor, func(t *testing.T) {
			t.Parallel()
			row := tc.row.envelope()
			rows := []supplychainmodel.AffectedPackage{
				{FactID: "z-vendor", Source: tc.vendor, AdvisoryID: "VND-1", Ecosystem: "os", PackageID: tc.packageID, PURL: tc.packageID + tc.query, Name: "curl"},
				{FactID: "a-osv", Source: "osv", AdvisoryID: "OSV-1", Ecosystem: "os", PackageID: tc.packageID, PURL: tc.packageID + tc.query, Name: "curl"},
				{FactID: "b-nvd", Source: "nvd", AdvisoryID: "NVD-1", Ecosystem: "os", PackageID: tc.packageID, Name: "curl"},
			}
			index, _, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{row})
			if err != nil {
				t.Fatalf("build index: %v", err)
			}
			for _, reversed := range []bool{false, true} {
				pkgs := slices.Clone(rows)
				if reversed {
					slices.Reverse(pkgs)
				}
				marked, _, ok := withJoinableOSRepresentative(pkgs, rows[2], index)
				if !ok {
					t.Fatal("no fact was marked")
				}
				if got := representativeAffectedPackage(marked).FactID; got != "z-vendor" {
					t.Fatalf("representative = %q, want the vendor's fact", got)
				}
			}
		})
	}
}

// TestRepresentativeHonorsTheJoinMark pins the one rule ahead of source order: the
// marked fact wins whatever the sources rank, wherever it sits, and no mark changes
// nothing.
func TestRepresentativeHonorsTheJoinMark(t *testing.T) {
	t.Parallel()
	ranked := supplychainmodel.AffectedPackage{FactID: "ranked", Source: "debian", Ecosystem: "deb"}
	joining := supplychainmodel.AffectedPackage{FactID: "joining", Source: "ubuntu", Ecosystem: "deb"}
	if got := representativeAffectedPackage([]supplychainmodel.AffectedPackage{ranked, joining}).FactID; got != "ranked" {
		t.Fatalf("with no mark, representative = %q, want source order's ranked fact", got)
	}
	joining.JoinRepresentative = true
	for _, order := range [][]supplychainmodel.AffectedPackage{{ranked, joining}, {joining, ranked}} {
		if got := representativeAffectedPackage(order).FactID; got != "joining" {
			t.Fatalf("representative = %q, want the marked fact", got)
		}
	}
}

// TestLanguageGroupKeepsItsPickAcrossEcosystemSpellings pins the mixed group the
// producers do write: GLAD keeps the raw slug prefix ("gem") and OSV the normalized
// name ("rubygems") on one package id. Every language spelling ranks under one
// table, so the ranking change moves nothing here (#9990).
func TestLanguageGroupKeepsItsPickAcrossEcosystemSpellings(t *testing.T) {
	t.Parallel()
	fact := func(factID, source, ecosystem string) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{FactID: factID, Source: source, Ecosystem: ecosystem}
	}
	for _, spellings := range [][2]string{{"gem", "rubygems"}, {"packagist", "composer"}, {"golang", "go"}, {"", "npm"}} {
		glad, osv := fact("glad", "glad", spellings[0]), fact("osv", "osv", spellings[1])
		for _, order := range [][]supplychainmodel.AffectedPackage{{glad, osv}, {osv, glad}} {
			if got := representativeAffectedPackage(order).FactID; got != "glad" {
				t.Errorf("%v: representative of %s,%s = %q, want the language table's glad", spellings, order[0].FactID, order[1].FactID, got)
			}
		}
	}
}
