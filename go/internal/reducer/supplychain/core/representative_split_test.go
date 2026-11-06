// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestSplitDirectBlankGroupSplitsOnlyTheRuledGroups is the contract of the split in ruling
// 9990-f6, on one index that holds both rows. A blank or whitespace ecosystem, or a
// whitespace version, splits. A spelled os or dpkg, a consumer, a group whose facts all
// join a versioned row, and rows that are all versioned or all versionless do not. The
// input index is unchanged and the versionless index has no outcome map.
func TestSplitDirectBlankGroupSplitsOnlyTheRuledGroups(t *testing.T) {
	t.Parallel()
	joining := repJoinOtherFact(repDirectOtherID, "osv", "os")
	joining.Payload["purl"] = repJoinPURL
	consume := []facts.Envelope{packageConsumptionFactWithRange("consume-9990", repJoinPackageID, "repository:r_9990", repJoinVersion)}
	both := mixedTopologies[4].rows
	cases := []struct {
		name       string
		own, other facts.Envelope
		rows       []mixedRow
		extra      []facts.Envelope
		want       bool
	}{
		{"blank, a fact cannot join, mixed", repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu"), both, nil, true},
		{"whitespace ecosystem", repJoinVendorFixFact(" "), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu"), both, nil, true},
		{"whitespace version", repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu"), mixedTopologies[5].rows, nil, true},
		{"spelled os", repJoinOwnFact("os"), repJoinOtherFact(repDirectOtherID, "nvd", ""), both, nil, false},
		{"spelled dpkg", repJoinOwnFact("dpkg"), repJoinOtherFact(repDirectOtherID, "nvd", "os"), both, nil, false},
		{"consumer", repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu"), both, consume, false},
		{"every fact joins a versioned row", repJoinVendorFixFact(""), joining, both, nil, false},
		{"all versioned", repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu"), []mixedRow{{"row-v", repJoinVersion, "a", mixedDigestA}, {"row-w", repJoinVersion, "b", mixedDigestB}}, nil, false},
		{"all versionless", repJoinVendorFixFact(""), repJoinOtherFact(repDirectOtherID, "osv", "ubuntu"), []mixedRow{{"row-n", "", "a", mixedDigestA}, {"row-m", "", "b", mixedDigestB}}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, analyses := mixedEnvs(c.rows)
			input := append(append(append([]facts.Envelope{c.own, c.other}, rows...), analyses...), c.extra...)
			index, _, err := buildSupplyChainImpactIndexWithQuarantine(input)
			if err != nil {
				t.Fatal(err)
			}
			own, err := supplyChainAffectedPackageFromEnvelope(c.own)
			if err != nil {
				t.Fatal(err)
			}
			other, err := supplyChainAffectedPackageFromEnvelope(c.other)
			if err != nil {
				t.Fatal(err)
			}
			before := fmt.Sprint(index.osPackages)
			versioned, versionless, split := splitDirectBlankGroupByRowVersion([]supplychainmodel.AffectedPackage{own, other}, own, index)
			if split != c.want {
				t.Fatalf("split = %v, want %v", split, c.want)
			}
			if fmt.Sprint(index.osPackages) != before {
				t.Fatal("the input index changed")
			}
			if !split {
				return
			}
			for _, rows := range versioned.osPackages {
				for _, row := range rows {
					if strings.TrimSpace(row.InstalledVersion) == "" {
						t.Fatalf("versioned holds %s", row.FactID)
					}
				}
			}
			if len(versionless.osPackages) == 0 || versionless.osRepresentativeOutcomes != nil {
				t.Fatalf("versionless index = %#v", versionless.osRepresentativeOutcomes)
			}
		})
	}
}

// splitHostFact is an affected-package fact with a blank ecosystem for the curl package of
// one host vendor ("debian" or "ubuntu"). Its purl and package id join that vendor's rows
// only.
func splitHostFact(factID, source, host, release string) facts.Envelope {
	e := repJoinVendorFixFact("")
	e.FactID = factID
	e.Payload["source"] = source
	e.Payload["advisory_id"] = "ADV-" + factID
	e.Payload["package_id"] = "pkg:deb/" + host + "/curl"
	e.Payload["purl"] = "pkg:deb/" + host + "/curl?arch=amd64&distro=" + host + "-" + release
	return e
}

// splitHostRow is the installed row of curl on one host, in its own scope.
func splitHostRow(row installedOSRow, factID string) facts.Envelope {
	e := row.envelope()
	e.FactID, e.ScopeID, e.GenerationID = factID, "image://"+factID, "g-"+factID
	return e
}

// splitPackages reads the affected packages of fact envelopes.
func splitPackages(t *testing.T, input ...facts.Envelope) []supplychainmodel.AffectedPackage {
	t.Helper()
	var out []supplychainmodel.AffectedPackage
	for _, e := range input {
		p, err := supplyChainAffectedPackageFromEnvelope(e)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// TestSplitConditionEReadsTheVersionedPart keeps condition e on the versioned part (ruling
// 9990-f6, 2.3e). A fact that joins only a row with no version joins no row of the versioned
// part, so the group splits; read against the whole index it would join a row and the group
// would stay whole (review 5, mutant X7).
func TestSplitConditionEReadsTheVersionedPart(t *testing.T) {
	t.Parallel()
	own := splitHostFact("own", "debian", "debian", "12")
	onlyVersionless := splitHostFact("only-versionless", "ubuntu", "ubuntu", "22.04")
	index, _, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{
		splitHostRow(debianCurl("12", repJoinVersion), "row-v"),
		splitHostRow(debianCurl("12", ""), "row-n"),
		splitHostRow(ubuntuCurl("22.04", ""), "row-m"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pkgs := splitPackages(t, own, onlyVersionless)
	if !hasMatchingOSPackage(pkgs[1], index) {
		t.Fatal("fixture: the second fact must join the row with no version")
	}
	if _, _, split := splitDirectBlankGroupByRowVersion(pkgs, pkgs[0], index); !split {
		t.Fatal("a fact that joins only a row with no version joins no row of the versioned part, so the group splits")
	}
}

// TestSplitPartsHoldRowsUnderEveryFactsKeys keeps the parts built from the lookup keys of
// every fact of the group, not only the representative's (review 5, mutant X18). The second
// fact joins a versioned row of another host; the versioned part must still hold it.
func TestSplitPartsHoldRowsUnderEveryFactsKeys(t *testing.T) {
	t.Parallel()
	own := splitHostFact("own", "debian", "debian", "12")
	other := splitHostFact("other", "ubuntu", "ubuntu", "22.04")
	cannotJoin := repJoinOtherFact("cannot-join", "osv", "debian")
	index, _, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{
		splitHostRow(debianCurl("12", repJoinVersion), "row-v"),
		splitHostRow(debianCurl("12", ""), "row-n"),
		splitHostRow(ubuntuCurl("22.04", repJoinVersion), "row-u"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pkgs := splitPackages(t, own, other, cannotJoin)
	versioned, _, split := splitDirectBlankGroupByRowVersion(pkgs, pkgs[0], index)
	if !split {
		t.Fatal("want a split: one fact joins no row of the versioned part")
	}
	if !hasMatchingOSPackage(pkgs[1], versioned) {
		t.Fatal("the versioned part lost the row only the second fact joins")
	}
}
