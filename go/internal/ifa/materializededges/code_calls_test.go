// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import "testing"

// TestCodeCallEdgeKeyIsInjective is the regression test for a real,
// currently-live weakening: codeCallEdgeKey joined its three components with
// a raw "|" and no escaping, so a source ending in "|X" and a target of "Y"
// rendered identically to a source of just "S" and a target of "X|Y" (type
// and leading component held fixed). code_calls is already proven live on
// both the ifa-determinism and ifa-fault-injection gates; a "|" inside a
// code-entity uid -- legal, since these uids are path-derived and "|" is a
// valid POSIX path character -- would silently collapse two of its five
// expected edges, with the exact-set assertion unable to tell.
func TestCodeCallEdgeKeyIsInjective(t *testing.T) {
	t.Parallel()

	keyA := codeCallEdgeKey("CALLS", "S|X", "Y")
	keyB := codeCallEdgeKey("CALLS", "S", "X|Y")
	if keyA == keyB {
		t.Fatalf("distinct (source, target) pairs collided onto one key %q; the exact-set comparison would silently merge them", keyA)
	}
}

// TestCompareCodeCallExpectedEdgesDetectsInjectivityDependentMismatch proves
// compareCodeCallExpectedEdges -- the production comparison function
// resolveCodeCallMaterializedEdges (code_calls' live vacuity guard) actually
// calls, not a re-implementation of it -- depends on codeCallEdgeKey's
// injectivity to do its job. expected names one edge (source "S|X", target
// "Y"); actual produces a DIFFERENT edge (source "S", target "X|Y"): under
// the pre-fix raw "|" join both rendered the same key, so this exact input
// shape would have reported no mismatch even though the extractor derived a
// structurally different edge than the fixture named -- a live weakening on
// code_calls, which is already proven on both live gates.
func TestCompareCodeCallExpectedEdgesDetectsInjectivityDependentMismatch(t *testing.T) {
	t.Parallel()

	expected := []ExpectedEdge{
		{RelationshipType: "CALLS", SourceEntityID: "S|X", TargetEntityID: "Y"},
	}
	actual := []ExpectedEdge{
		{RelationshipType: "CALLS", SourceEntityID: "S", TargetEntityID: "X|Y"},
	}

	if mismatch := compareCodeCallExpectedEdges("test-odu", expected, actual); mismatch == "" {
		t.Fatal("compareCodeCallExpectedEdges reported no mismatch for two structurally distinct edges; a non-injective codeCallEdgeKey would let this collapse silently")
	}
}
