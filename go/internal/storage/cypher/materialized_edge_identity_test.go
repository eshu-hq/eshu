// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestMaterializedEdgeIdentityByFamilyIsTotal locks materializedEdgeIdentityByFamily
// to reducer.MaterializedEdgeFamilies() in both directions: a family enumerated
// there with no identity entry here would let the Ifá bijection drift guard
// (materialized_edge_identity_drift_test.go) silently skip it, and a stale
// entry here for a family no longer enumerated would mislead a reader into
// thinking it is still gated.
func TestMaterializedEdgeIdentityByFamilyIsTotal(t *testing.T) {
	t.Parallel()

	families := reducer.MaterializedEdgeFamilies()
	want := make(map[string]struct{}, len(families))
	for _, family := range families {
		want[family] = struct{}{}
	}

	var missing, extra []string
	for family := range want {
		if _, ok := materializedEdgeIdentityByFamily[family]; !ok {
			missing = append(missing, family)
		}
	}
	for family := range materializedEdgeIdentityByFamily {
		if _, ok := want[family]; !ok {
			extra = append(extra, family)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("reducer.MaterializedEdgeFamilies() names %v with no materializedEdgeIdentityByFamily entry; register an explicit entry (empty if the family MERGEs on endpoints alone) before it can be gated", missing)
	}
	if len(extra) > 0 {
		t.Errorf("materializedEdgeIdentityByFamily declares %v, which reducer.MaterializedEdgeFamilies() no longer enumerates; remove the stale entry", extra)
	}
}

// TestMaterializedEdgeIdentityPropertiesFailsClosed proves an unregistered
// family returns an error rather than an empty, silently-passing set.
func TestMaterializedEdgeIdentityPropertiesFailsClosed(t *testing.T) {
	t.Parallel()

	if _, err := MaterializedEdgeIdentityProperties("no_such_family"); err == nil {
		t.Fatal("MaterializedEdgeIdentityProperties(no_such_family) returned no error, want a fail-closed error")
	}
	if _, err := MaterializedEdgeIdentityProperties("codeowners_ownership_edges"); err != nil {
		t.Fatalf("MaterializedEdgeIdentityProperties(codeowners_ownership_edges) returned an error for a registered family: %v", err)
	}
}

// TestMaterializedEdgeIdentityPropertiesReturnsACopy stops a caller mutating
// the package-level table through the returned map or its property slices.
func TestMaterializedEdgeIdentityPropertiesReturnsACopy(t *testing.T) {
	t.Parallel()

	first, err := MaterializedEdgeIdentityProperties("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeIdentityProperties: %v", err)
	}
	first["DECLARES_CODEOWNER"][0] = "mutated"
	first["EXTRA_TYPE"] = []string{"should not persist"}

	second, err := MaterializedEdgeIdentityProperties("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeIdentityProperties: %v", err)
	}
	want := map[string][]string{"DECLARES_CODEOWNER": {"pattern", "source_path"}}
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("MaterializedEdgeIdentityProperties second call = %v, want %v (mutation through the first call's return leaked into the table)", second, want)
	}
}

// TestCodeownersAndSubmoduleIdentityPropertiesMatchTheirMergeKeys pins the
// two gated families' declared identity against the literal property names
// their MERGE templates use, so a reviewer can see the exact expected values
// without cross-referencing the writer files.
func TestCodeownersAndSubmoduleIdentityPropertiesMatchTheirMergeKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		family string
		want   map[string][]string
	}{
		{"codeowners_ownership_edges", map[string][]string{"DECLARES_CODEOWNER": {"pattern", "source_path"}}},
		{"submodule_pin_edges", map[string][]string{"PINS_SUBMODULE": {"path"}}},
	}
	for _, tc := range cases {
		t.Run(tc.family, func(t *testing.T) {
			t.Parallel()
			got, err := MaterializedEdgeIdentityProperties(tc.family)
			if err != nil {
				t.Fatalf("MaterializedEdgeIdentityProperties(%s): %v", tc.family, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MaterializedEdgeIdentityProperties(%s) = %v, want %v", tc.family, got, tc.want)
			}
		})
	}
}
