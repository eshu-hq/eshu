// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"regexp"
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
