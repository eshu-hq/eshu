// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
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

// identityMergePropertyOpen finds a relationship variable declaration in
// MERGE-pattern edge-bracket position (`-[var:TYPE {`) carrying an inline
// property map — the shape a family's write template uses when it folds a
// property into its relationship MERGE key. It requires the `-[` edge-bracket
// prefix so a node pattern with properties (`(repo:Repository {id: ...})`) is
// never mistaken for a relationship MERGE.
var identityMergePropertyOpen = regexp.MustCompile(`-\[\s*[A-Za-z_][A-Za-z0-9_]*\s*:\s*[A-Z][A-Z0-9_]*\s*\{`)

// identityMatchingBrace returns the index in cypher of the '}' that closes
// the '{' at index open, counting nested '(){}[]' so a property value
// containing a function call or literal balances correctly, or -1 if it
// never closes. Mirrors matchingParenIndex in
// unwind_bare_match_set_gate_test.go for the same reason: a naive
// non-greedy match on the first '}' would stop inside a nested literal.
func identityMatchingBrace(cypher string, open int) int {
	depth := 0
	for i := open; i < len(cypher); i++ {
		switch cypher[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if cypher[i] == '}' && depth == 0 {
				return i
			}
		}
	}
	return -1
}

// identityTopLevelSegments splits value on commas at bracket depth zero, so a
// comma inside a nested call or literal (e.g. `coalesce(row.a, row.b)`) does
// not split a single property assignment into two.
func identityTopLevelSegments(value string) []string {
	var segments []string
	depth := 0
	start := 0
	for i, r := range value {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				segments = append(segments, value[start:i])
				start = i + 1
			}
		}
	}
	segments = append(segments, value[start:])
	return segments
}

// identityPropertyKey extracts the property name from one `key: value`
// segment of a Cypher property map.
func identityPropertyKey(segment string) (string, bool) {
	idx := strings.IndexByte(segment, ':')
	if idx < 0 {
		return "", false
	}
	key := strings.TrimSpace(segment[:idx])
	if key == "" {
		return "", false
	}
	return key, true
}

// identityPropertiesFromCypher extracts the sorted property names of the
// (assumed single) relationship-MERGE property map in cypher — a family's own
// held IdentityCypher write template — or nil if its MERGE keys on endpoints
// alone. This inspects only the one known write-path const each family
// declares in materialized_edge_families.go, never package doc comments or
// unrelated source, so — unlike the source-directory scan this replaces — it
// has no comment-exclusion failure mode.
func identityPropertiesFromCypher(cypher string) []string {
	loc := identityMergePropertyOpen.FindStringIndex(cypher)
	if loc == nil {
		return nil
	}
	openBrace := loc[1] - 1
	closeBrace := identityMatchingBrace(cypher, openBrace)
	if closeBrace < 0 {
		return nil
	}
	var props []string
	for _, seg := range identityTopLevelSegments(cypher[openBrace+1 : closeBrace]) {
		if key, ok := identityPropertyKey(seg); ok {
			props = append(props, key)
		}
	}
	sort.Strings(props)
	return props
}

// TestIdentityPropertiesFromCypher is the unit-level proof for the scanner
// itself: the flat property-bearing shape (DECLARES_CODEOWNER), a
// property-less relationship MERGE (must extract nothing), a node property
// map (must not be mistaken for a relationship MERGE), and a nested-call
// property value.
func TestIdentityPropertiesFromCypher(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cypher string
		want   []string
	}{
		{
			name:   "flat two-property identity",
			cypher: `MERGE (repo)-[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]->(team)`,
			want:   []string{"pattern", "source_path"},
		},
		{
			name:   "property-less relationship MERGE extracts nothing",
			cypher: `MERGE (source)-[rel:CALLS]->(target)`,
			want:   nil,
		},
		{
			name:   "node property map is not mistaken for a relationship MERGE",
			cypher: `MERGE (repo:Repository {id: row.repo_id})`,
			want:   nil,
		},
		{
			name:   "nested-call property value does not split the segment early",
			cypher: `MERGE (a)-[rel:EXAMPLE {key: coalesce(row.a, row.b)}]->(b)`,
			want:   []string{"key"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := identityPropertiesFromCypher(tc.cypher)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("identityPropertiesFromCypher(%q) = %#v, want %#v", tc.cypher, got, tc.want)
			}
		})
	}
}

// TestSingleTypeFamilyIdentityMatchesWriteCypher is the drift guard: for
// every singleTypeMaterializedEdgeFamilies entry, it extracts the
// relationship-MERGE property map straight out of the family's own held
// IdentityCypher write template and asserts it matches the declared
// IdentityProperties exactly — in both directions, since a set-equality
// mismatch fires whether the write Cypher gained a property the declaration
// omits or the declaration names one the write Cypher no longer has. This is
// the const-reference sibling of TestMaterializedEdgeFamilyRegistryMatchesItsRetract
// above: it holds the real write const (not a copy of its text or a scan of
// the whole package's sources), so it cannot mistake a doc comment that
// quotes example Cypher for a production template.
func TestSingleTypeFamilyIdentityMatchesWriteCypher(t *testing.T) {
	t.Parallel()

	for family, entry := range singleTypeMaterializedEdgeFamilies {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			got := identityPropertiesFromCypher(entry.IdentityCypher)
			want := append([]string(nil), entry.IdentityProperties...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("family %q: write Cypher MERGE identity properties = %v, want %v (materializedEdgeFamily.IdentityProperties declaration)", family, got, want)
			}
		})
	}
}

// TestMaterializedEdgeIdentityByFamilyIsTotal locks the union of
// singleTypeMaterializedEdgeFamilies and materializedEdgeIdentityByFamily to
// reducer.MaterializedEdgeFamilies() in both directions: a family enumerated
// there with no identity declaration would let
// TestSingleTypeFamilyIdentityMatchesWriteCypher silently skip it, and a
// stale declaration for a family no longer enumerated would mislead a reader
// into thinking it is still gated.
func TestMaterializedEdgeIdentityByFamilyIsTotal(t *testing.T) {
	t.Parallel()

	families := reducer.MaterializedEdgeFamilies()
	want := make(map[string]struct{}, len(families))
	for _, family := range families {
		want[family] = struct{}{}
	}

	have := make(map[string]struct{}, len(singleTypeMaterializedEdgeFamilies)+len(materializedEdgeIdentityByFamily))
	for family := range singleTypeMaterializedEdgeFamilies {
		have[family] = struct{}{}
	}
	for family := range materializedEdgeIdentityByFamily {
		have[family] = struct{}{}
	}

	var missing, extra []string
	for family := range want {
		if _, ok := have[family]; !ok {
			missing = append(missing, family)
		}
	}
	for family := range have {
		if _, ok := want[family]; !ok {
			extra = append(extra, family)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("reducer.MaterializedEdgeFamilies() names %v with no identity declaration; register it in singleTypeMaterializedEdgeFamilies or materializedEdgeIdentityByFamily (empty if the family MERGEs on endpoints alone) before it can be gated", missing)
	}
	if len(extra) > 0 {
		t.Errorf("identity declared for %v, which reducer.MaterializedEdgeFamilies() no longer enumerates; remove the stale entry", extra)
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
// the package-level tables through the returned map or its property slices.
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
