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
			got, reselected, ok := withJoinableOSRepresentative(group, group[tc.representative], tc.index)
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
			_, _, ok := withJoinableOSRepresentative([]supplychainmodel.AffectedPackage{blankOwn, other}, blankOwn, index)
			if ok != tc.wantNarrowed {
				t.Fatalf("narrowed = %v, want %v", ok, tc.wantNarrowed)
			}
		})
	}
}
