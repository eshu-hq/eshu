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
