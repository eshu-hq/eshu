// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// valueFlowPinnedCases maps each value-flow read case to the production
// statement it must run verbatim.
var valueFlowPinnedCases = map[string]string{
	valueFlowWorkloadRowsCaseName: reducer.ValueFlowCloudSinkWorkloadRowsCypher,
	valueFlowTargetsCaseName:      reducer.ValueFlowCloudSinkTargetsByPairCypher,
}

// TestValueFlowReadCasesEqualTheProductionStatements pins each value-flow read
// case to its production statement by equality rather than by fragments.
//
// Equality replaced a fragment list that was defeated three separate times by
// mutations it did not enumerate. A fragment list can only bound the mutations
// someone thought of; equality bounds all of them, in both directions -- a
// change to a production statement fails this too, which is the point, since
// the case would then be proving a query nobody runs.
//
// The import runs backendconformance -> reducer. The reverse would be a cycle
// (reducer is reached from internal/storage/cypher, which this package imports).
func TestValueFlowReadCasesEqualTheProductionStatements(t *testing.T) {
	for name, production := range valueFlowPinnedCases {
		c, ok := readCaseByName(name)
		if !ok {
			t.Errorf("read case %q is absent from DefaultReadCorpus", name)
			continue
		}
		if c.Cypher != production {
			t.Errorf("read case %q has drifted from the production statement.\ncase:\n%s\n\nproduction:\n%s",
				name, c.Cypher, production)
		}
		if c.Capability != CapabilityPathTraversal {
			t.Errorf("read case %q capability = %q, want %q", name, c.Capability, CapabilityPathTraversal)
		}
	}
	// The production call sites bind these Go types; a different list type
	// would prove a different driver binding than production uses.
	rows, _ := readCaseByName(valueFlowWorkloadRowsCaseName)
	if uids, ok := rows.Parameters["function_uids"].([]string); !ok || len(uids) == 0 {
		t.Errorf("function_uids must bind as a non-empty []string; got %T", rows.Parameters["function_uids"])
	}
	targets, _ := readCaseByName(valueFlowTargetsCaseName)
	if pairs, ok := targets.Parameters["pairs"].([]map[string]any); !ok || len(pairs) == 0 {
		t.Errorf("pairs must bind as a non-empty []map[string]any; got %T", targets.Parameters["pairs"])
	}
}

// readCaseParam extracts the $parameters a pinned read statement binds.
var readCaseParam = regexp.MustCompile(`\$([a-z_][a-z0-9_]*)`)

// TestValueFlowSeedWritesEveryValueTheReadCasesBind asserts that every value a
// read case binds is one the seed actually writes, so a typo in a bound id
// fails here instead of as an unexplained row mismatch on the live gate.
func TestValueFlowSeedWritesEveryValueTheReadCasesBind(t *testing.T) {
	// Membership over the seed's parameter VALUES, never containment in its
	// concatenated text: "backend-conformance" is a substring of nearly every
	// fixture id and would satisfy a containment check.
	seeded := make(map[string]struct{})
	for _, c := range DefaultWriteCorpus() {
		if c.Name != valueFlowWriteCaseName {
			continue
		}
		for _, st := range c.Statements {
			for _, v := range st.Parameters {
				for _, sv := range flattenSeedValue(v) {
					seeded[sv] = struct{}{}
				}
			}
		}
	}
	if len(seeded) == 0 {
		t.Fatalf("write case %q is absent or binds nothing", valueFlowWriteCaseName)
	}

	for name := range valueFlowPinnedCases {
		read, ok := readCaseByName(name)
		if !ok {
			t.Fatalf("read case %q is absent", name)
		}
		for _, m := range readCaseParam.FindAllStringSubmatch(read.Cypher, -1) {
			bound, ok := read.Parameters[m[1]]
			if !ok {
				t.Errorf("%s references $%s but binds no such parameter", name, m[1])
				continue
			}
			for _, v := range flattenSeedValue(bound) {
				if _, ok := seeded[v]; !ok {
					t.Errorf("%s binds %s %q, which the seed never writes", name, m[1], v)
				}
			}
		}
	}
}

func flattenSeedValue(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []string:
		return t
	case []any:
		var out []string
		for _, e := range t {
			out = append(out, flattenSeedValue(e)...)
		}
		return out
	case map[string]any:
		var out []string
		for _, e := range t {
			out = append(out, flattenSeedValue(e)...)
		}
		return out
	case []map[string]any:
		var out []string
		for _, e := range t {
			out = append(out, flattenSeedValue(e)...)
		}
		return out
	}
	// Never silently skip an uncased type. A bool or numeric bound parameter
	// would otherwise go unchecked.
	return []string{fmt.Sprintf("%v", v)}
}

// readCaseLabel and readCaseRelType extract the labels and relationship types
// a pinned read statement matches on.
var (
	readCaseLabel   = regexp.MustCompile(`:([A-Z][A-Za-z0-9_]*)\s*[){ ]`)
	readCaseRelType = regexp.MustCompile(`\[[a-zA-Z_]*:([A-Z_][A-Z0-9_]*)\]`)
)

// TestValueFlowSeedWritesWhatTheReadCasesMatchOn derives its expectation from
// the pinned statements instead of listing seed shapes by hand, so it cannot go
// stale independently of them. It checks that each label and relationship type
// is NAMED in the seed, not that it is wired; the exact-row assertion on the
// live gate is what proves the wiring.
func TestValueFlowSeedWritesWhatTheReadCasesMatchOn(t *testing.T) {
	var seed string
	for _, c := range DefaultWriteCorpus() {
		if c.Name == valueFlowWriteCaseName {
			for _, st := range c.Statements {
				seed += st.Cypher + "\n"
			}
		}
	}
	if seed == "" {
		t.Fatalf("write case %q is absent or has no statements", valueFlowWriteCaseName)
	}
	for name, read := range valueFlowPinnedCases {
		for _, m := range readCaseRelType.FindAllStringSubmatch(read, -1) {
			if !containsToken(seed, m[1]) {
				t.Errorf("seed does not write [%s], which %s matches on", m[1], name)
			}
		}
		for _, m := range readCaseLabel.FindAllStringSubmatch(read, -1) {
			if !containsToken(seed, m[1]) {
				t.Errorf("seed does not write :%s, which %s matches on", m[1], name)
			}
		}
	}
}

func containsToken(haystack, token string) bool {
	re := regexp.MustCompile(`[:\[]` + regexp.QuoteMeta(token) + `\b`)
	return re.MatchString(haystack)
}
