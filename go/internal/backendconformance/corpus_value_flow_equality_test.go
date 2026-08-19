// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"bytes"
	"os"
	"testing"
)

// productionStatementPath is the reducer source that owns the statement this
// package's value-flow read case exists to prove.
const productionStatementPath = "../reducer/value_flow_cloud_sink_loader.go"

// productionStatementMarker opens the production constant's raw string literal.
const productionStatementMarker = "const valueFlowCloudSinkTargetsCypher = `"

// TestValueFlowReadCaseEqualsTheProductionStatement pins the value-flow read
// case to the production statement by equality rather than by fragments.
//
// It reads the constant out of the reducer source instead of importing it, for
// two reasons. The constant is unexported, and exporting it would widen a
// package API to satisfy a test. And the import is impossible anyway: this
// package imports internal/storage/cypher, which imports internal/reducer, so a
// reducer-side test that imported this package would be an import cycle.
//
// Equality replaces a fragment list that lived in the sibling test file and was
// defeated three separate times by mutations it did not enumerate -- decomposing
// the multi-hop MATCH into chained single-hop clauses, dropping the
// WHERE size(workloads) = 1 filter, and truncating the RETURN to one column.
// Each keeps the case name and MinRows, still returns a row on a conforming
// backend, and therefore proves nothing while looking green. A fragment list can
// only bound the mutations someone thought of; equality bounds all of them.
//
// Known limit, stated so nobody over-trusts it: this reads the production file
// from disk, so a `go test -overlay` mutation of that file is invisible to it.
// It is a drift guard for real edits, not a mutation oracle for the reducer
// side. Mutations of the case itself, which is an ordinary runtime value, are
// caught normally.
func TestValueFlowReadCaseEqualsTheProductionStatement(t *testing.T) {
	t.Setenv(valueFlowCasesEnv, "1")

	source, err := os.ReadFile(productionStatementPath)
	if err != nil {
		t.Fatalf("read production statement source: %v", err)
	}
	start := bytes.Index(source, []byte(productionStatementMarker))
	if start < 0 {
		t.Fatalf("%s no longer declares %s; update this guard rather than deleting it",
			productionStatementPath, productionStatementMarker)
	}
	rest := source[start+len(productionStatementMarker):]
	end := bytes.IndexByte(rest, '`')
	if end < 0 {
		t.Fatalf("production statement literal in %s is unterminated", productionStatementPath)
	}
	production := string(rest[:end])

	var found bool
	for _, c := range DefaultReadCorpus() {
		if c.Name != valueFlowReadCaseName {
			continue
		}
		found = true
		if c.Cypher != production {
			t.Errorf("value-flow read case has drifted from the production statement.\n"+
				"This case exists to prove THAT query runs on a backend, so any difference\n"+
				"means it proves something else.\ncase:\n%s\n\nproduction:\n%s", c.Cypher, production)
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
