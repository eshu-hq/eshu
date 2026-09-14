// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExternalMethodCallDoesNotReachSameNamedPackageMethod proves that call
// targets are resolved by receiver, not only by their bare selector name.
func TestExternalMethodCallDoesNotReachSameNamedPackageMethod(t *testing.T) {
	t.Parallel()

	const source = `package cypher

import (
	"context"
	"os"
)

const relationshipTemplate = "MERGE (a)-[rel:REVIEW_PROBE_FLOWS_TO]->(b)"
const aliasedRelationshipTemplate = relationshipTemplate

type CanonicalNodeWriter struct{}

func (CanonicalNodeWriter) Write() {
	_ = relationshipTemplate
}

func ExternalWrite() {
	temporary, _ := os.CreateTemp("", "trigger")
	defer temporary.Close()
	_, _ = temporary.Write(nil)
}

func AliasedWrite() {
	_ = aliasedRelationshipTemplate
}

type localWriter struct{}

func (writer localWriter) LocalWrite() {
	writer.writeEdges()
}

func (localWriter) writeEdges() {
	packageEdgeHelper()
}

func LocalValueWrite() {
	other := localWriter{}
	other.writeEdges()
}

func LocalPointerWrite() {
	other := &localWriter{}
	other.writeEdges()
}

type BackpressureObserver interface {
	ObserveBackpressureWait()
}

type localObserver struct{}

func (localObserver) ObserveBackpressureWait() {
	packageEdgeHelper()
}

func LocalInterfaceWrite(observer BackpressureObserver) {
	observer.ObserveBackpressureWait()
}

type Statement struct{ Query string }

type Executor interface {
	Execute(context.Context, Statement) error
}

type localExecutor struct{}

func (localExecutor) Execute(context.Context, Statement) error {
	packageEdgeHelper()
	return nil
}

func TerminalBoundaryNodeOnly(ctx context.Context, executor Executor) {
	_ = executor.Execute(ctx, Statement{Query: "MATCH (n) RETURN n"})
}

func ReachableDynamic(callback func()) {
	callback()
}

func ReachableIndexedDynamic(callbacks []func()) {
	callbacks[0]()
}

func NodeOnlyWithUnreachableDynamic() {
	_ = 1
}

func unusedDynamic(callback func()) {
	callback()
}

func ShadowedNodeOnly() {
	relationshipTemplate := "MATCH (n) RETURN n"
	_ = relationshipTemplate
}

func packageEdgeHelper() {
	_ = relationshipTemplate
}
`
	dir := t.TempDir()
	const module = "module example.com/cypherscan\n\ngo 1.26.6\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o600); err != nil {
		t.Fatalf("write scan fixture module: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "writer.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write scan fixture: %v", err)
	}

	classifications := classifyCypherPorts(
		parseCypherPackage(t, dir),
		map[string]struct{}{
			"AliasedWrite":                   {},
			"ExternalWrite":                  {},
			"LocalInterfaceWrite":            {},
			"LocalPointerWrite":              {},
			"LocalValueWrite":                {},
			"LocalWrite":                     {},
			"NodeOnlyWithUnreachableDynamic": {},
			"ReachableDynamic":               {},
			"ReachableIndexedDynamic":        {},
			"ShadowedNodeOnly":               {},
			"TerminalBoundaryNodeOnly":       {},
		},
	)
	byPort := make(map[string]cypherPortClassification, len(classifications))
	for _, classification := range classifications {
		byPort[classification.Port] = classification
	}
	if got := byPort["ExternalWrite"]; got.WritesEdges {
		t.Errorf("ExternalWrite reaches relationship Cypher through os.File.Write: %q", got.Evidence)
	}
	if got := byPort["AliasedWrite"]; !got.WritesEdges {
		t.Error("AliasedWrite did not resolve the package constant alias to relationship Cypher")
	}
	if got := byPort["LocalWrite"]; !got.WritesEdges {
		t.Error("LocalWrite did not reach the same-receiver writeEdges helper")
	}
	if got := byPort["LocalValueWrite"]; !got.WritesEdges {
		t.Error("LocalValueWrite did not reach writeEdges through a local value receiver")
	}
	if got := byPort["LocalPointerWrite"]; !got.WritesEdges {
		t.Error("LocalPointerWrite did not reach writeEdges through a local pointer receiver")
	}
	if got := byPort["LocalInterfaceWrite"]; !got.WritesEdges {
		t.Error("LocalInterfaceWrite did not follow the package-local implementation of an otherwise terminal interface seam")
	}
	if got := byPort["TerminalBoundaryNodeOnly"]; got.WritesEdges || len(got.UnknownRefs) != 0 {
		t.Errorf("TerminalBoundaryNodeOnly followed the explicit Executor.Execute boundary: %+v", got)
	}
	if got := byPort["ReachableDynamic"]; len(got.UnknownRefs) == 0 {
		t.Error("ReachableDynamic was classified without reporting its reachable function-value call")
	}
	if got := byPort["ReachableIndexedDynamic"]; len(got.UnknownRefs) == 0 {
		t.Error("ReachableIndexedDynamic was classified without reporting its indexed function-value call")
	}
	if got := byPort["NodeOnlyWithUnreachableDynamic"]; len(got.UnknownRefs) != 0 {
		t.Errorf("NodeOnlyWithUnreachableDynamic was poisoned by an unreachable function-value call: %v", got.UnknownRefs)
	}
	if got := byPort["ShadowedNodeOnly"]; got.WritesEdges {
		t.Errorf("ShadowedNodeOnly bound a local shadow to the package relationship template: %q", got.Evidence)
	}
}

// TestRelationshipMergeReadsNestedNodePatternParens holds the
// relationship-MERGE reader to node patterns that contain parentheses of their
// own (#6181).
//
// The reader matched a node pattern with `[^()]*`, so the first `(` INSIDE the
// pattern ended the match early and the whole clause stopped looking like a
// relationship merge. `MERGE (n:Label {id: coalesce($a, $b)})-[r:TYPE]->(m)` is
// ordinary Cypher — a merge keyed on a coalesced identity — and it read as
// node-only. A port whose only write site is such a template is then classified
// node-only, its family never enters the enumeration, and the ledger has no row
// to be missing: the false-green direction the whole guard exists to prevent,
// reproduced inside it.
//
// No production template is written this way today, which is exactly why the
// case has to be stated here rather than derived from the tree: a fixture taken
// from the tree passes with the hole open. The negative cases below are the
// other half — widening the reader until it reports a relationship merge in a
// MATCH, or in a relationship-index DDL, would trade a silent miss for a loud
// wrong answer on every retract port in the package.
func TestRelationshipMergeReadsNestedNodePatternParens(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		cypher string
		want   bool
	}{
		{
			name:   "flat node pattern",
			cypher: "MERGE (a:Thing)-[rel:REVIEW_PROBE_FLOWS_TO]->(b:Thing)",
			want:   true,
		},
		{
			name:   "function call in the node pattern",
			cypher: "MERGE (n:Label {id: coalesce($a, $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   true,
		},
		{
			name:   "nested function calls in the node pattern",
			cypher: "MERGE (n:Label {id: coalesce(toString($a), $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   true,
		},
		{
			// A quoted `)` must not close the node pattern. Quote-unaware, the
			// walk ends at the paren inside the string, never sees the trailing
			// -[rel:...]->, and reports node-only.
			name:   "quoted paren in a property value",
			cypher: `MERGE (n:Repo {path: "a/b)c"})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)`,
			want:   true,
		},
		{
			// Same, single-quoted, and with an escaped quote inside the value
			// so the escape handling is exercised rather than assumed.
			name:   "escaped quote and paren in a property value",
			cypher: `MERGE (n:Repo {path: 'a\'b)c'})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)`,
			want:   true,
		},
		{
			name:   "left-pointing relationship after a call",
			cypher: "MERGE (n:Label {id: coalesce($a, $b)})<-[rel:REVIEW_PROBE_FLOWS_TO]-(m)",
			want:   true,
		},
		{
			name:   "CREATE with a call in the node pattern",
			cypher: "CREATE (n {id: coalesce($a, $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   true,
		},
		{
			name:   "node-only merge whose pattern calls a function",
			cypher: "MERGE (n:Label {id: coalesce($a, $b)})",
			want:   false,
		},
		{
			name:   "MATCH is not a merge",
			cypher: "MATCH (n:Label {id: coalesce($a, $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m) DELETE rel",
			want:   false,
		},
		{
			name:   "relationship index DDL is not a merge",
			cypher: "CREATE INDEX probe_rel IF NOT EXISTS FOR ()-[rel:REVIEW_PROBE_FLOWS_TO]-() ON (rel.id)",
			want:   false,
		},
		{
			name:   "unbalanced node pattern resolves nothing",
			cypher: "MERGE (n:Label {id: coalesce($a, $b)-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			line, merges := relationshipMergeLine(tc.cypher)
			if merges != tc.want {
				t.Fatalf("relationshipMergeLine(%q) merges = %t, want %t", tc.cypher, merges, tc.want)
			}
			if tc.want && line != tc.cypher {
				t.Errorf("evidence line = %q, want %q", line, tc.cypher)
			}
		})
	}
}

// TestNestedNodePatternMergeIsVisibleThroughAPortDeclaration is the same hole
// stated end to end: a package-level template written the way the case above
// describes has to reach the scan's classification, not merely the line reader.
func TestNestedNodePatternMergeIsVisibleThroughAPortDeclaration(t *testing.T) {
	t.Parallel()

	const source = `package cypher

const nestedRelationship = "MERGE (n:Label {id: coalesce($a, $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)"

func NestedWrite() { _ = nestedRelationship }
`
	classifications := classifyCypherPorts(
		parseCypherPackage(t, writeCypherScanFixture(t, source)),
		map[string]struct{}{"NestedWrite": {}},
	)
	if len(classifications) != 1 || !classifications[0].WritesEdges {
		t.Fatalf("NestedWrite classification = %+v, want one relationship-writing port", classifications)
	}
}
