// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// relationshipMergeIdentity is one relationship-MERGE property map found in
// this package's own Cypher template constants: `-[var:TYPE {props}]`.
type relationshipMergeIdentity struct {
	RelationshipType string
	// Properties is the deduplicated set of property names the MERGE keys
	// on, in first-encountered order.
	Properties []string
	SourceFile string
}

// relationshipMergePropertyOpen finds a relationship variable declaration in
// MERGE-pattern edge-bracket position (`-[var:TYPE {`) carrying an inline
// property map. It deliberately requires the `-[` edge-bracket prefix so a
// node pattern with properties (`(repo:Repository {id: ...})`) is never
// mistaken for a relationship MERGE.
var relationshipMergePropertyOpen = regexp.MustCompile(`-\[\s*[A-Za-z_][A-Za-z0-9_]*\s*:\s*([A-Z][A-Z0-9_]*)\s*\{`)

// matchingBraceIndex returns the index in value of the '}' that closes the
// '{' at index open, counting nested '(){}[]' so a property value containing
// a function call or literal balances correctly, or -1 if it never closes.
// Mirrors matchingParenIndex in unwind_bare_match_set_gate_test.go for the
// same reason: a naive non-greedy match on the first '}' would stop inside a
// nested literal.
func matchingBraceIndex(value string, open int) int {
	depth := 0
	for i := open; i < len(value); i++ {
		switch value[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if value[i] == '}' && depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTopLevelSegments splits value on commas at bracket depth zero, so a
// comma inside a nested call or literal (e.g. `coalesce(row.a, row.b)`) does
// not split a single property assignment into two.
func splitTopLevelSegments(value string) []string {
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

// propertyKeyFromSegment extracts the property name from one `key: value`
// segment of a Cypher property map.
func propertyKeyFromSegment(segment string) (string, bool) {
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

// extractRelationshipMergeIdentities scans one Cypher template's literal text
// for every relationship-MERGE property map and returns the relationship
// type plus its deduplicated, order-preserved property names. It is a text
// scan over Cypher template constants, not a general Cypher parser: every
// relationship-MERGE identity map in this package today is a flat `{key:
// value, ...}` list (see the bijection this test proves against
// materializedEdgeIdentityByFamily), so brace/paren/bracket depth counting
// is sufficient. A future template that nested a literal map as a property
// VALUE inside an identity map would need a richer scanner; this one still
// fails loud (unbalanced braces) rather than silently mis-parsing.
func extractRelationshipMergeIdentities(value string) []relationshipMergeIdentity {
	var out []relationshipMergeIdentity
	matches := relationshipMergePropertyOpen.FindAllStringSubmatchIndex(value, -1)
	for _, m := range matches {
		relType := value[m[2]:m[3]]
		openBrace := m[1] - 1 // the pattern ends with '\{', so this is its index.
		closeBrace := matchingBraceIndex(value, openBrace)
		if closeBrace < 0 {
			continue
		}
		inner := value[openBrace+1 : closeBrace]
		seen := map[string]bool{}
		var props []string
		for _, seg := range splitTopLevelSegments(inner) {
			key, ok := propertyKeyFromSegment(seg)
			if !ok || seen[key] {
				continue
			}
			seen[key] = true
			props = append(props, key)
		}
		out = append(out, relationshipMergeIdentity{RelationshipType: relType, Properties: props})
	}
	return out
}

// scanRelationshipMergeIdentities parses every non-test .go file directly in
// this package directory and collects every relationship-MERGE property map
// found in any string constant's literal value. Mirrors
// TestNoUnwindBareMatchThenSetCyphersInPackage's file-walk shape
// (unwind_bare_match_set_gate_test.go): only *ast.ValueSpec literal values are
// inspected, so a doc comment that quotes example Cypher (several writer
// files in this package do, deliberately, in prose) is never mistaken for a
// production template.
func scanRelationshipMergeIdentities(t *testing.T) []relationshipMergeIdentity {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	var found []relationshipMergeIdentity
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			valueSpec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, value := range valueSpec.Values {
				lit, ok := value.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				for _, id := range extractRelationshipMergeIdentities(text) {
					id.SourceFile = name
					found = append(found, id)
				}
			}
			return true
		})
	}
	return found
}

// edgeTypeSetFromReasonMap projects a writer registry's type->reason map onto
// the type-set shape gatedFamilyEdgeTypes needs. Deliberately duplicated from
// ifa.edgeTypeSet: this package cannot import go/internal/ifa (ifa imports
// cypher, and the reverse would cycle), so the family->edge-type resolution
// this test needs is rebuilt here from this package's own exported
// registries only.
func edgeTypeSetFromReasonMap(registry map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(registry))
	for edgeType := range registry {
		out[edgeType] = struct{}{}
	}
	return out
}

// gatedFamilyEdgeTypes resolves one of the 14 reducer.MaterializedEdgeFamilies()
// families to its registry edge-type set, mirroring
// ifa.MaterializedEdgeDomainEdgeTypes's switch using only this package's own
// exported registries (see edgeTypeSetFromReasonMap's doc comment for why the
// logic is duplicated rather than imported).
func gatedFamilyEdgeTypes(family string) (map[string]struct{}, error) {
	switch family {
	case "sql_relationships":
		return edgeTypeSetFromReasonMap(SQLRelationshipMaterializedEdgeTypes()), nil
	case "code_calls":
		return edgeTypeSetFromReasonMap(CodeCallMaterializedEdgeTypes()), nil
	case "inheritance_edges":
		return edgeTypeSetFromReasonMap(InheritanceMaterializedEdgeTypes()), nil
	case "repo_dependency":
		return edgeTypeSetFromReasonMap(RepoDependencyMaterializedEdgeTypes()), nil
	default:
		if reg, ok := SingleTypeMaterializedEdgeTypes(family); ok {
			return edgeTypeSetFromReasonMap(reg), nil
		}
		return nil, fmt.Errorf("cypher: no materialized-edge family registered for domain %q", family)
	}
}

// gatedFamilyEdgeTypeOwners returns the relationship-type -> owning-families
// map for every reducer.MaterializedEdgeFamilies() family today. A type can
// have more than one owning family: DEPENDS_ON is written Repository->
// Repository by repo_dependency and Workload->Workload by
// workload_dependency, so type alone does not partition them
// (MaterializedEdgeEndpointLabels is what does, for the live assert-edges
// path — irrelevant to this test, which only compares relationship-property
// identity). A relationship type absent from this map belongs to a
// direct-materialization domain excluded from the 14-family enumeration
// (documentation of that exclusion:
// go/internal/reducer/materialized_edge_families.go) — DERIVED_FROM,
// TAINT_FLOWS_TO, PUBLISHES, and BUILT_FROM are the four such types this
// package's writers use today. If one of those domains is ever added to
// reducer.MaterializedEdgeFamilies(), its types appear here automatically and
// TestMaterializedEdgeIdentityMatchesRelationshipMergeSource starts requiring
// a matching materializedEdgeIdentityByFamily declaration for it — no second
// hand-edit needed to wire the trip.
func gatedFamilyEdgeTypeOwners(t *testing.T) map[string][]string {
	t.Helper()
	owners := map[string][]string{}
	for _, family := range reducer.MaterializedEdgeFamilies() {
		types, err := gatedFamilyEdgeTypes(family)
		if err != nil {
			t.Fatalf("gatedFamilyEdgeTypes(%s): %v", family, err)
		}
		for edgeType := range types {
			owners[edgeType] = append(owners[edgeType], family)
		}
	}
	return owners
}

// sortedCopy returns a sorted copy of props so comparisons are order
// independent: materializedEdgeIdentityByFamily documents its declared order
// for readability, but a MERGE's actual identity is a set.
func sortedCopy(props []string) []string {
	out := append([]string(nil), props...)
	sort.Strings(out)
	return out
}

// TestMaterializedEdgeIdentityMatchesRelationshipMergeSource is the drift
// guard: it parses this package's own non-test .go sources, extracts every
// relationship-MERGE property map, and asserts a BIJECTION with
// materializedEdgeIdentityByFamily, scoped to the 14
// reducer.MaterializedEdgeFamilies() families.
//
// Direction (i): every extracted (type, props) whose type belongs to a gated
// family must be declared EXACTLY in the registry. This is what catches a
// writer changing its MERGE key (e.g. dropping source_path from
// DECLARES_CODEOWNER) without updating the registry the Ifá assert-edges live
// gate reads.
//
// Direction (ii): every declared non-empty (family, type, props) entry must
// be found by the scan. This is what catches a stale registry declaration
// left behind after a writer's MERGE key changes underneath it.
//
// Five relationship types this package writes today (DERIVED_FROM,
// TAINT_FLOWS_TO, PUBLISHES, BUILT_FROM) belong to direct-materialization
// domains the 14-family enumeration deliberately excludes
// (go/internal/reducer/materialized_edge_families.go), so they are NOT
// asserted against the registry — see gatedFamilyEdgeTypeOwners's doc comment
// for why they still trip automatically if that enumeration ever grows to
// include them.
func TestMaterializedEdgeIdentityMatchesRelationshipMergeSource(t *testing.T) {
	t.Parallel()

	owners := gatedFamilyEdgeTypeOwners(t)
	extracted := scanRelationshipMergeIdentities(t)

	bySourceType := map[string][]string{}
	for _, id := range extracted {
		props := sortedCopy(id.Properties)
		if prior, ok := bySourceType[id.RelationshipType]; ok {
			if !reflect.DeepEqual(prior, props) {
				t.Fatalf(
					"relationship type %q has inconsistent MERGE identity properties across occurrences: %v vs %v (file %s)",
					id.RelationshipType, prior, props, id.SourceFile,
				)
			}
			continue
		}
		bySourceType[id.RelationshipType] = props
	}

	// Direction (i).
	for relType, sourceProps := range bySourceType {
		families, owned := owners[relType]
		if !owned {
			continue
		}
		for _, family := range families {
			declared, err := MaterializedEdgeIdentityProperties(family)
			if err != nil {
				t.Fatalf("family %q owns %q but has no materializedEdgeIdentityByFamily entry: %v", family, relType, err)
			}
			want := sortedCopy(declared[relType])
			if !reflect.DeepEqual(want, sourceProps) {
				t.Errorf(
					"family %q type %q: source MERGE identity properties %v do not match materializedEdgeIdentityByFamily's %v",
					family, relType, sourceProps, want,
				)
			}
		}
	}

	// Direction (ii).
	for family, types := range materializedEdgeIdentityByFamily {
		for relType, declaredProps := range types {
			if len(declaredProps) == 0 {
				continue
			}
			want := sortedCopy(declaredProps)
			got, found := bySourceType[relType]
			if !found {
				t.Errorf(
					"family %q declares identity properties %v for %q but no relationship-MERGE with that shape was found in this package's source",
					family, want, relType,
				)
				continue
			}
			if !reflect.DeepEqual(want, got) {
				t.Errorf("family %q type %q: materializedEdgeIdentityByFamily declares %v, source MERGE has %v", family, relType, want, got)
			}
		}
	}
}

// TestExtractRelationshipMergeIdentities is the unit-level proof for the
// scanner itself, covering the flat single-line shape (DECLARES_CODEOWNER),
// the multi-line shape (DERIVED_FROM), a property-less relationship MERGE
// (which must extract nothing), and a nested-call property value.
func TestExtractRelationshipMergeIdentities(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  []relationshipMergeIdentity
	}{
		{
			name:  "flat single-line two-property identity",
			value: `MERGE (repo)-[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]->(team)`,
			want: []relationshipMergeIdentity{
				{RelationshipType: "DECLARES_CODEOWNER", Properties: []string{"pattern", "source_path"}},
			},
		},
		{
			name: "multi-line two-property identity",
			value: `MERGE (img)-[rel:DERIVED_FROM {
  scope_id: row.scope_id,
  evidence_source: row.evidence_source
}]->(base)`,
			want: []relationshipMergeIdentity{
				{RelationshipType: "DERIVED_FROM", Properties: []string{"scope_id", "evidence_source"}},
			},
		},
		{
			name:  "property-less relationship MERGE extracts nothing",
			value: `MERGE (source)-[rel:CALLS]->(target)`,
			want:  nil,
		},
		{
			name:  "nested-call property value does not split the segment early",
			value: `MERGE (a)-[rel:EXAMPLE {key: coalesce(row.a, row.b)}]->(b)`,
			want: []relationshipMergeIdentity{
				{RelationshipType: "EXAMPLE", Properties: []string{"key"}},
			},
		},
		{
			name:  "node property map is not mistaken for a relationship MERGE",
			value: `MERGE (repo:Repository {id: row.repo_id})`,
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractRelationshipMergeIdentities(tc.value)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractRelationshipMergeIdentities(%q) = %#v, want %#v", tc.value, got, tc.want)
			}
		})
	}
}
