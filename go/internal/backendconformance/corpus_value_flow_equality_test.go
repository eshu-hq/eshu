// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestValueFlowReadCaseEqualsTheProductionStatement pins the value-flow read
// case to the production statement by equality rather than by fragments.
//
// Equality replaces a fragment list that lived in the sibling test file and was
// defeated three separate times by mutations it did not enumerate: decomposing
// the multi-hop MATCH into chained single-hop clauses, dropping the
// WHERE size(workloads) = 1 filter, and truncating the RETURN to one column.
// Each keeps the case name and MinRows, still returns a row on a conforming
// backend, and therefore proves nothing while looking green. A fragment list can
// only bound the mutations someone thought of; equality bounds all of them, in
// both directions -- a change to the production statement fails this too, which
// is the point, since the case would then be proving a query nobody runs.
//
// The import runs backendconformance -> reducer. The reverse would be a cycle
// (reducer is reached from internal/storage/cypher, which this package imports),
// which is why the constant is exported here rather than the equality living on
// the reducer side.
func TestValueFlowReadCaseEqualsTheProductionStatement(t *testing.T) {
	t.Setenv(valueFlowCasesEnv, "1")

	var found bool
	for _, c := range DefaultReadCorpus() {
		if c.Name != valueFlowReadCaseName {
			continue
		}
		found = true
		if c.Cypher != reducer.ValueFlowCloudSinkTargetsCypher {
			t.Errorf("value-flow read case has drifted from the production statement.\n"+
				"This case exists to prove THAT query runs on a backend, so any difference\n"+
				"means it proves something else.\ncase:\n%s\n\nproduction:\n%s",
				c.Cypher, reducer.ValueFlowCloudSinkTargetsCypher)
		}
		if c.Capability != CapabilityPathTraversal {
			t.Errorf("value-flow read case capability = %q, want %q. The case is classified by "+
				"what the statement IS -- a bounded multi-hop traversal -- not by which of its "+
				"divergences trips first, so the label holds still as upstream fixes them.",
				c.Capability, CapabilityPathTraversal)
		}
		uids, ok := c.Parameters["function_uids"].([]string)
		if !ok {
			t.Fatalf("function_uids must bind as []string, matching the production call site; got %T",
				c.Parameters["function_uids"])
		}
		if len(uids) == 0 {
			t.Fatal("function_uids is empty, which returns no rows on any backend")
		}
	}
	if !found {
		t.Fatalf("read case %q is absent from DefaultReadCorpus with %s set",
			valueFlowReadCaseName, valueFlowCasesEnv)
	}
}

// readCaseParam extracts the $parameters the pinned read statement binds.
var readCaseParam = regexp.MustCompile(`\$([a-z_][a-z0-9_]*)`)

// TestValueFlowSeedWritesEveryValueTheReadCaseBinds asserts that every value the
// read case binds is one the seed actually writes.
//
// Without this, binding function_uids to a uid the seed never creates passes every
// hermetic test — and the live result is a read that matches nothing on BOTH
// backends, which is indistinguishable from the NornicDB defect this case exists to
// detect. That confusion is precisely what the stale-proof note in evidence-notes.md
// warns about.
func TestValueFlowSeedWritesEveryValueTheReadCaseBinds(t *testing.T) {
	t.Setenv(valueFlowCasesEnv, "1")

	var read ReadCase
	for _, c := range DefaultReadCorpus() {
		if c.Name == valueFlowReadCaseName {
			read = c
		}
	}
	if read.Name == "" {
		t.Fatalf("read case %q is absent", valueFlowReadCaseName)
	}

	var seed string
	for _, c := range DefaultWriteCorpus() {
		if c.Name == valueFlowWriteCaseName {
			for _, st := range c.Statements {
				seed += st.Cypher + "\n"
				for _, v := range st.Parameters {
					seed += renderSeedValue(v) + "\n"
				}
			}
		}
	}
	if seed == "" {
		t.Fatalf("write case %q is absent or has no statements", valueFlowWriteCaseName)
	}

	for _, m := range readCaseParam.FindAllStringSubmatch(read.Cypher, -1) {
		bound, ok := read.Parameters[m[1]]
		if !ok {
			t.Errorf("read case references $%s but binds no such parameter", m[1])
			continue
		}
		for _, v := range flattenSeedValue(bound) {
			if !strings.Contains(seed, v) {
				t.Errorf("read case binds %s %q, which the seed never writes; "+
					"the read would return zero rows on every backend", m[1], v)
			}
		}
	}
}

func renderSeedValue(v any) string {
	return strings.Join(flattenSeedValue(v), "\n")
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
	}
	return nil
}

// readCaseLabel and readCaseRelType extract the labels and relationship types
// the pinned read statement matches on.
var (
	readCaseLabel   = regexp.MustCompile(`:([A-Z][A-Za-z0-9_]*)\s*[){ ]`)
	readCaseRelType = regexp.MustCompile(`\[[a-zA-Z_]*:([A-Z_][A-Z0-9_]*)\]`)
)

// TestValueFlowSeedWritesWhatTheReadCaseMatchesOn derives its expectation from
// the read statement instead of listing seed shapes by hand.
//
// Equality pins the read; nothing pinned that the seed writes what the read
// matches on, so deleting a seed statement passed every hermetic test. A
// hand-written list of expected shapes would be the same fragment-list failure
// one layer down, so the expectation is computed from the query equality already
// guarantees and cannot go stale independently of it.
//
// Known limit, stated so nobody over-trusts it: this checks that the shapes are
// NAMED in the seed, not that they are WIRED. It cannot tell "creates" from
// "matches" — deleting a node's MERGE leaves its label present in the later MATCH
// clauses — and it does not check property targets. Three mutations pass it and
// each turns the case into "returns zero rows on every backend", which is
// indistinguishable from the defect it detects. The live Neo4j lane is what proves
// the wiring, which is why #6192's positive control is load-bearing rather than
// decorative.
func TestValueFlowSeedWritesWhatTheReadCaseMatchesOn(t *testing.T) {
	t.Setenv(valueFlowCasesEnv, "1")

	var read string
	for _, c := range DefaultReadCorpus() {
		if c.Name == valueFlowReadCaseName {
			read = c.Cypher
		}
	}
	if read == "" {
		t.Fatalf("read case %q is absent; the seed guard has nothing to derive from", valueFlowReadCaseName)
	}

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

	for _, m := range readCaseRelType.FindAllStringSubmatch(read, -1) {
		if !containsToken(seed, m[1]) {
			t.Errorf("seed does not write [%s], which the read case matches on", m[1])
		}
	}
	for _, m := range readCaseLabel.FindAllStringSubmatch(read, -1) {
		if !containsToken(seed, m[1]) {
			t.Errorf("seed does not write :%s, which the read case matches on", m[1])
		}
	}
}

func containsToken(haystack, token string) bool {
	re := regexp.MustCompile(`[:\[]` + regexp.QuoteMeta(token) + `\b`)
	return re.MatchString(haystack)
}
