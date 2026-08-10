// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// relTypeAlternation pulls every [rel:A|B] alternative out of a Cypher template.
var relTypeAlternation = regexp.MustCompile(`\[(?:\w*):([A-Z_][A-Z_|]*)\]`)

// retractRelTypes returns the set of relationship types a retract template
// deletes, read out of the template text itself.
func retractRelTypes(t *testing.T, family, cypher string) map[string]struct{} {
	t.Helper()
	matches := relTypeAlternation.FindAllStringSubmatch(cypher, -1)
	if len(matches) == 0 {
		t.Fatalf("family %q: retract template names no relationship type; a retract that reaps nothing leaves every edge of this family stale after reprojection:\n%s", family, cypher)
	}
	out := map[string]struct{}{}
	for _, m := range matches {
		for _, relType := range strings.Split(m[1], "|") {
			if relType = strings.TrimSpace(relType); relType != "" {
				out[relType] = struct{}{}
			}
		}
	}
	return out
}

// TestMaterializedEdgeFamilyRegistryMatchesItsRetract pins every single-type
// family's declared edge types against what its retract actually deletes (#5543).
//
// The two live in different files and different shapes, and nothing else stops
// them drifting. Drift is silent in both directions: a type written but not
// retracted survives every reprojection as stale truth, and a type retracted but
// missing from the registry is one the Ifá baseline gate stops asserting while
// still calling the family exhaustive.
//
// The alternation is extracted from the production const held in the table, not
// compared against a literal copied into this test. A copy would pin the test
// file against itself and keep passing after the retract changed.
func TestMaterializedEdgeFamilyRegistryMatchesItsRetract(t *testing.T) {
	t.Parallel()

	for family, entry := range singleTypeMaterializedEdgeFamilies {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			retracted := retractRelTypes(t, family, entry.RetractCypher)

			var missingFromRegistry, missingFromRetract []string
			for relType := range retracted {
				if _, ok := entry.EdgeTypes[relType]; !ok {
					missingFromRegistry = append(missingFromRegistry, relType)
				}
			}
			for relType := range entry.EdgeTypes {
				if _, ok := retracted[relType]; !ok {
					missingFromRetract = append(missingFromRetract, relType)
				}
			}
			sort.Strings(missingFromRegistry)
			sort.Strings(missingFromRetract)

			if len(missingFromRegistry) > 0 {
				t.Errorf("retract deletes %v but the registry omits them; the Ifá baseline gate would ignore those edges while calling %s exhaustive",
					missingFromRegistry, family)
			}
			if len(missingFromRetract) > 0 {
				t.Errorf("registry names %v but the retract does not delete them; those edges would survive every reprojection as stale truth",
					missingFromRetract)
			}
		})
	}
}

// TestSingleTypeFamiliesDeclareExactlyOneType keeps the table honest about what
// it is for.
//
// Multi-type families (code_calls, inheritance_edges) deliberately live next to
// their writers because their reasoning spans several templates. If a family
// here ever grows a second type, that reasoning has to move rather than being
// quietly appended to a table whose whole premise is one type per family.
func TestSingleTypeFamiliesDeclareExactlyOneType(t *testing.T) {
	t.Parallel()

	for family, entry := range singleTypeMaterializedEdgeFamilies {
		if len(entry.EdgeTypes) != 1 {
			t.Errorf("family %q declares %d edge types; move it beside its writer with its own registry and reasoning, like code_calls and inheritance_edges",
				family, len(entry.EdgeTypes))
		}
	}
}

// TestSingleTypeMaterializedEdgeTypesFailsClosed proves an unregistered family
// reports absence rather than an empty set.
//
// A caller that mistook "no entry" for "no edge types" would make the live gate
// assert nothing and pass any graph vacuously — the exact false-green the #5543
// waiver rows exist to prevent.
func TestSingleTypeMaterializedEdgeTypesFailsClosed(t *testing.T) {
	t.Parallel()

	if got, ok := SingleTypeMaterializedEdgeTypes("no_such_family"); ok {
		t.Errorf("unregistered family reported ok=true with %v; it must fail closed", got)
	}
	if _, ok := SingleTypeMaterializedEdgeTypes("runs_in"); !ok {
		t.Error("registered family runs_in reported ok=false")
	}
}

// TestSingleTypeMaterializedEdgeTypesReturnsACopy stops a caller shrinking what
// the live gate asserts for the rest of the process.
func TestSingleTypeMaterializedEdgeTypesReturnsACopy(t *testing.T) {
	t.Parallel()

	first, ok := SingleTypeMaterializedEdgeTypes("runs_in")
	if !ok {
		t.Fatal("runs_in is not registered")
	}
	delete(first, "RUNS_IN")

	second, _ := SingleTypeMaterializedEdgeTypes("runs_in")
	if _, ok := second["RUNS_IN"]; !ok {
		t.Error("deleting from a returned map mutated the package registry; the accessor must return a copy")
	}
}
