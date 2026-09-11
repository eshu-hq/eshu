// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"reflect"
	"testing"
)

func TestNormalizedSupplyChainImpactMissingEvidenceLazyFastPath(t *testing.T) {
	t.Parallel()

	row := SupplyChainImpactFindingRow{
		MissingEvidence: []string{"advisory evidence missing", "runtime evidence missing"},
	}
	got := normalizedSupplyChainImpactMissingEvidence(&row)
	if !reflect.DeepEqual(got, row.MissingEvidence) {
		t.Fatalf("normalized missing evidence = %#v, want %#v", got, row.MissingEvidence)
	}
	if &got[0] != &row.MissingEvidence[0] {
		t.Fatal("already-normalized missing evidence was copied, want backing slice reuse")
	}
}

func TestNormalizedSupplyChainImpactMissingEvidenceFallbacksPreserveContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		row  SupplyChainImpactFindingRow
		want []string
	}{
		{
			name: "trims sorts drops blanks and deduplicates",
			row: SupplyChainImpactFindingRow{
				MissingEvidence: []string{" runtime evidence missing ", "", "advisory evidence missing", "advisory evidence missing"},
			},
			want: []string{"advisory evidence missing", "runtime evidence missing"},
		},
		{
			name: "present catalog evidence and resolved anchor remove both stale reasons",
			row: SupplyChainImpactFindingRow{
				ServiceIDs:   []string{"service:example-api"},
				EvidencePath: []string{serviceCatalogCorrelationFactKind},
				MissingEvidence: []string{
					ServiceCatalogAnchorMissingReason,
					ServiceCatalogCorrelationMissingReason,
					"runtime evidence missing",
				},
			},
			want: []string{"runtime evidence missing"},
		},
		{
			name: "present catalog evidence without an anchor rewrites and deduplicates",
			row: SupplyChainImpactFindingRow{
				EvidencePath: []string{serviceCatalogCorrelationFactKind},
				MissingEvidence: []string{
					ServiceCatalogCorrelationMissingReason,
					ServiceCatalogAnchorMissingReason,
				},
			},
			want: []string{ServiceCatalogAnchorMissingReason},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizedSupplyChainImpactMissingEvidence(&tc.row)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalized missing evidence = %#v, want %#v", got, tc.want)
			}
		})
	}
}
