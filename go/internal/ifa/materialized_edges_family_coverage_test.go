// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// materializedEdgeFamiliesUnderUmbrella lists every family the #5543 umbrella
// tracks, so "all of them resolve" is asserted against a written-down roster
// rather than against whatever happens to be registered.
//
// Deriving this list from the registry would make the test circular: removing a
// family would shrink both sides and stay green. sql_relationships is included
// because it is the reference family the other thirteen are modelled on.
var materializedEdgeFamiliesUnderUmbrella = []string{
	"sql_relationships",
	"code_calls",
	"codeowners_ownership_edges",
	"deployable_unit_edges",
	"documentation_edges",
	"handles_route",
	"inheritance_edges",
	"invokes_cloud_action",
	"rationale_edges",
	"repo_dependency",
	"runs_in",
	"shell_exec",
	"submodule_pin_edges",
	"workload_dependency",
}

// TestEveryUmbrellaFamilyResolves is the #5543 completion criterion for the
// registry layer: the live baseline gate can address every family.
//
// Until a family resolves here, `eshu-ifa assert-edges -domain <family>` exits
// with an error and no Odù, guard, or expected-edge set can green it on
// ifa-determinism. That is what blocked all twelve non-SQL families.
func TestEveryUmbrellaFamilyResolves(t *testing.T) {
	t.Parallel()

	for _, family := range materializedEdgeFamiliesUnderUmbrella {
		types, err := MaterializedEdgeDomainEdgeTypes(family)
		if err != nil {
			t.Errorf("family %q does not resolve: %v", family, err)
			continue
		}
		if len(types) == 0 {
			t.Errorf("family %q resolved to an empty type set; the live gate would assert nothing and pass any graph vacuously", family)
		}
	}
}

// TestEveryRegisteredEdgeTypeIsCanonical cross-checks every family's types
// against the canonical edgetype registry.
//
// This is the third independent source behind each derivation, after the write
// template and the retract alternation. It catches the failure the other two
// cannot: a typo'd or invented relationship type that is internally consistent
// between a registry and its retract but names an edge the graph never carries.
// Such a type would make the live gate assert a population that is always empty
// — a guard that can never fail.
func TestEveryRegisteredEdgeTypeIsCanonical(t *testing.T) {
	t.Parallel()

	canonical := map[string]struct{}{}
	for _, e := range edgetype.All() {
		canonical[string(e)] = struct{}{}
	}
	if len(canonical) == 0 {
		t.Fatal("edgetype.All() is empty; this test would pass vacuously")
	}

	for _, family := range materializedEdgeFamiliesUnderUmbrella {
		types, err := MaterializedEdgeDomainEdgeTypes(family)
		if err != nil {
			continue // TestEveryUmbrellaFamilyResolves owns that failure
		}
		var unknown []string
		for edgeTypeName := range types {
			if _, ok := canonical[edgeTypeName]; !ok {
				unknown = append(unknown, edgeTypeName)
			}
		}
		sort.Strings(unknown)
		if len(unknown) > 0 {
			t.Errorf("family %q registers %v, which the canonical edgetype registry does not define; the live gate would assert an always-empty population for them",
				family, unknown)
		}
	}
}
