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

// TestWithJoinableOSRepresentativeNarrowsOnlyTheRuledGroups pins the unit contract
// of the narrowing (#9990, arbiter ruling 9990-orderdep 3.5): which groups come back
// narrowed, whether the source-order representative joined, and that the input slice
// is returned as is, or is never marked.
func TestWithJoinableOSRepresentativeNarrowsOnlyTheRuledGroups(t *testing.T) {
	t.Parallel()
	fact := func(factID, source, ecosystem string, joins bool) supplychainmodel.AffectedPackage {
		pkg := supplychainmodel.AffectedPackage{
			FactID: factID, Source: source, Ecosystem: ecosystem, AdvisoryID: "ADV-" + factID,
			PackageID: repJoinPackageID, Name: "curl",
		}
		if joins {
			pkg.PURL = repJoinPURL
		}
		return pkg
	}
	versionless, _, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{ubuntuCurl("22.04", "").envelope()})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	for _, tc := range []struct {
		name           string
		index          supplyChainImpactIndex
		group          []supplychainmodel.AffectedPackage
		representative int
		wantOK         bool
		wantReselected bool
		wantFacts      []string
	}{
		{
			"blank representative joins, another fact cannot",
			osPackageRowForTest(t),
			[]supplychainmodel.AffectedPackage{fact("a-own", "ubuntu", "", true), fact("b-nvd", "nvd", "os", false)},
			0, true, false,
			[]string{"a-own"},
		},
		{
			"whitespace ecosystem counts as blank",
			osPackageRowForTest(t),
			[]supplychainmodel.AffectedPackage{fact("a-own", "ubuntu", " \t", true), fact("b-nvd", "nvd", "os", false)},
			0, true, false,
			[]string{"a-own"},
		},
		{
			"representative cannot join, another fact does",
			osPackageRowForTest(t),
			[]supplychainmodel.AffectedPackage{fact("a-nvd", "nvd", "os", false), fact("b-own", "ubuntu", "", true)},
			0, true, true,
			[]string{"b-own"},
		},
		{
			"every fact of a blank group joins",
			osPackageRowForTest(t),
			[]supplychainmodel.AffectedPackage{fact("a-own", "ubuntu", "", true), fact("b-own", "ubuntu", "", true)},
			0, false, false, nil,
		},
		{
			"a spelled ecosystem that joins has a base row",
			osPackageRowForTest(t),
			[]supplychainmodel.AffectedPackage{fact("a-own", "ubuntu", "os", true), fact("b-nvd", "nvd", "os", false)},
			0, false, false, nil,
		},
		{
			"a row with no installed version has a base row",
			versionless,
			[]supplychainmodel.AffectedPackage{fact("a-own", "ubuntu", "", true), fact("b-nvd", "nvd", "os", false)},
			0, false, false, nil,
		},
		{
			"no fact joins",
			osPackageRowForTest(t),
			[]supplychainmodel.AffectedPackage{fact("a-nvd", "nvd", "os", false), fact("b-nvd", "nvd", "deb", false)},
			0, false, false, nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			group := slices.Clone(tc.group)
			before := slices.Clone(group)
			got, reselected, ok, _ := withJoinableOSRepresentative(group, group[tc.representative], tc.index)
			if ok != tc.wantOK || reselected != tc.wantReselected {
				t.Fatalf("ok = %v, reselected = %v, want %v, %v", ok, reselected, tc.wantOK, tc.wantReselected)
			}
			if !reflect.DeepEqual(group, before) {
				t.Fatalf("the shared input slice was modified: %+v", group)
			}
			if !ok {
				if len(got) != len(group) || &got[0] != &group[0] {
					t.Fatalf("an unchanged group must return the input slice, got %+v", got)
				}
				return
			}
			var ids []string
			for _, pkg := range got {
				ids = append(ids, pkg.FactID)
			}
			if !slices.Equal(ids, tc.wantFacts) {
				t.Fatalf("narrowed to %v, want %v", ids, tc.wantFacts)
			}
			if !got[0].JoinRepresentative {
				t.Fatalf("the best-ranked joining fact is not marked: %+v", got[0])
			}
		})
	}
}

// TestNarrowedDirectGroupReadsEveryRowTheRepresentativeJoins pins condition c of the
// ruling on a host that holds two rows for the package: one row with no installed
// version keeps the whole group, whichever row sorts first, and a version of only
// white space counts as none.
func TestNarrowedDirectGroupReadsEveryRowTheRepresentativeJoins(t *testing.T) {
	t.Parallel()
	blankOwn := supplychainmodel.AffectedPackage{
		FactID: "a-own", Source: "ubuntu", AdvisoryID: "ADV-own", PackageID: repJoinPackageID, PURL: repJoinPURL, Name: "curl",
	}
	other := supplychainmodel.AffectedPackage{
		FactID: "b-nvd", Source: "nvd", Ecosystem: "os", AdvisoryID: "ADV-nvd", PackageID: repJoinPackageID, Name: "curl",
	}
	for _, tc := range []struct {
		name         string
		versionA     string
		versionB     string
		wantNarrowed bool
	}{
		{"both rows versioned", repJoinVersion, "1.0-4", true},
		{"the first row has no version", "", repJoinVersion, false},
		{"the last row has no version", repJoinVersion, "", false},
		{"white space is no version", repJoinVersion, " \t", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rowA, rowB := ubuntuCurl("22.04", tc.versionA).envelope(), ubuntuCurl("22.04", tc.versionB).envelope()
			rowA.FactID, rowB.FactID = "os-a", "os-b"
			rowB.Payload["arch"] = "arm64"
			index, _, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{rowA, rowB})
			if err != nil {
				t.Fatalf("build index: %v", err)
			}
			if rows := matchingOSPackageImpactPaths(blankOwn, index); len(rows) != 2 {
				t.Fatalf("the representative joins %d rows, want 2", len(rows))
			}
			_, _, ok, _ := withJoinableOSRepresentative([]supplychainmodel.AffectedPackage{blankOwn, other}, blankOwn, index)
			if ok != tc.wantNarrowed {
				t.Fatalf("narrowed = %v, want %v", ok, tc.wantNarrowed)
			}
		})
	}
}

// TestNarrowedDirectGroupReadsABlankRowThatDoesNotSortFirst keeps condition c reading
// every joined row, not only the first. The rows sort by their raw installed version,
// so a row whose version is a single space sorts after one that starts with a control
// character, and a check of the first row alone would miss the blank one (the review's
// witness for mutant N8).
func TestNarrowedDirectGroupReadsABlankRowThatDoesNotSortFirst(t *testing.T) {
	t.Parallel()
	blankOwn := supplychainmodel.AffectedPackage{
		FactID: "a-own", Source: "ubuntu", AdvisoryID: "ADV-own",
		PackageID: repJoinPackageID, PURL: repJoinPURL, Name: "curl",
	}
	other := supplychainmodel.AffectedPackage{
		FactID: "b-nvd", Source: "nvd", Ecosystem: "os", AdvisoryID: "ADV-nvd",
		PackageID: repJoinPackageID, Name: "curl",
	}
	rowA, rowB := ubuntuCurl("22.04", "\t1.0-3").envelope(), ubuntuCurl("22.04", " ").envelope()
	rowA.FactID, rowB.FactID = "os-a", "os-b"
	rowB.Payload["arch"] = "arm64"
	index, _, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{rowA, rowB})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := withJoinableOSRepresentative([]supplychainmodel.AffectedPackage{blankOwn, other}, blankOwn, index); ok {
		t.Fatal("narrowed a group whose joined rows include a row with a blank version")
	}
}

// TestWithJoinableDecidedOutcomeMatchesWalkedOutcome pins the fusion contract
// behind the quiet-host timing fix (#9990): the outcome
// withJoinableOSRepresentative settles while deciding the narrowing must equal
// the outcome recordOSRepresentativeOutcome counts by walking, and groups the
// narrowing cannot settle decide "" so the walked count stands.
func TestWithJoinableDecidedOutcomeMatchesWalkedOutcome(t *testing.T) {
	t.Parallel()
	fact := func(factID, source, ecosystem, packageID, name, purl string) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{
			FactID: factID, Source: source, Ecosystem: ecosystem, AdvisoryID: "ADV-" + factID,
			PackageID: packageID, Name: name, PURL: purl,
		}
	}
	foreign := func(factID string) supplychainmodel.AffectedPackage {
		return fact(factID, "nvd", "deb", "pkg:deb/debian/notinstalled", "notinstalled", "pkg:deb/debian/notinstalled")
	}
	identity := func(factID string) supplychainmodel.AffectedPackage {
		return fact(factID, "debian", "os", "os://debian/curl", "curl", "")
	}
	for _, tc := range []struct {
		name           string
		group          []supplychainmodel.AffectedPackage
		representative int
		wantDecided    string
		wantWalked     string
	}{
		{
			"narrowed direct blank decides joined",
			[]supplychainmodel.AffectedPackage{
				fact("a-own", "ubuntu", "", repJoinPackageID, "curl", repJoinPURL),
				fact("b-nvd", "nvd", "os", repJoinPackageID, "curl", ""),
			},
			0, osRepresentativeJoined, osRepresentativeJoined,
		},
		{
			"spelled representative that joins decides joined",
			[]supplychainmodel.AffectedPackage{
				fact("a-own", "ubuntu", "os", repJoinPackageID, "curl", repJoinPURL),
				fact("b-nvd", "nvd", "os", repJoinPackageID, "curl", ""),
			},
			0, osRepresentativeJoined, osRepresentativeJoined,
		},
		{
			"reselected decides reselected",
			[]supplychainmodel.AffectedPackage{
				fact("a-nvd", "nvd", "os", repJoinPackageID, "curl", ""),
				fact("b-own", "ubuntu", "os", repJoinPackageID, "curl", repJoinPURL),
			},
			0, osRepresentativeReselected, osRepresentativeReselected,
		},
		{
			"every fact joins decides joined",
			[]supplychainmodel.AffectedPackage{
				fact("a-own", "ubuntu", "", repJoinPackageID, "curl", repJoinPURL),
				fact("b-own", "ubuntu", "", repJoinPackageID, "curl", repJoinPURL),
			},
			0, osRepresentativeJoined, osRepresentativeJoined,
		},
		{
			"no fact joins decides no_joinable_representative",
			[]supplychainmodel.AffectedPackage{
				fact("a-nvd", "nvd", "os", repJoinPackageID, "curl", ""),
				fact("b-nvd", "nvd", "deb", repJoinPackageID, "curl", ""),
			},
			0, osRepresentativeNoJoinable, osRepresentativeNoJoinable,
		},
		{
			"no row under the keys decides no_installed_row",
			[]supplychainmodel.AffectedPackage{foreign("a-nvd"), foreign("b-nvd")},
			0, osRepresentativeNoInstalledRow, osRepresentativeNoInstalledRow,
		},
		{
			"an identity that meets no row decides no_joinable_identity",
			[]supplychainmodel.AffectedPackage{identity("a-debian"), identity("b-debian")},
			0, osRepresentativeNoJoinableIdentity, osRepresentativeNoJoinableIdentity,
		},
		{
			// Decision 4: an OSV fact whose deb purl names no known vendor
			// classifies to "" and never checks keys, while the walked count
			// checks keys regardless, so the narrowing decides nothing here.
			"a blank-vendor fact with rows under its keys decides nothing",
			[]supplychainmodel.AffectedPackage{
				fact("a-osv", "osv", "os", repJoinPackageID, "curl", "pkg:deb/kali/curl"),
				fact("b-osv", "osv", "os", repJoinPackageID, "curl", "pkg:deb/kali/curl"),
			},
			0, "", osRepresentativeNoJoinable,
		},
		{
			"a lone fact decides nothing and the walked count stands",
			[]supplychainmodel.AffectedPackage{
				fact("a-own", "ubuntu", "os", repJoinPackageID, "curl", repJoinPURL),
			},
			0, "", osRepresentativeJoined,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probed := osPackageRowForTest(t)
			probed.osRepresentativeOutcomes = map[osRepresentativeOutcomeKey]int{}
			_, reselected, _, decided := withJoinableOSRepresentative(tc.group, tc.group[tc.representative], probed)
			if decided != tc.wantDecided {
				t.Fatalf("decided = %q, want %q", decided, tc.wantDecided)
			}
			probed.recordOSRepresentativeOutcome(tc.group, tc.group[tc.representative], reselected, false, "")
			if len(probed.osRepresentativeOutcomes) != 1 {
				t.Fatalf("walked %d outcomes, want 1: %v", len(probed.osRepresentativeOutcomes), probed.osRepresentativeOutcomes)
			}
			for key := range probed.osRepresentativeOutcomes {
				if key.outcome != tc.wantWalked {
					t.Fatalf("walked = %q, want %q", key.outcome, tc.wantWalked)
				}
				if decided != "" && key.outcome != decided {
					t.Fatalf("walked = %q but decided = %q", key.outcome, decided)
				}
			}
		})
	}
}
