// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resolutionparity

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// dartSelfLoopRecursionCallGraphFixture is the real-pipeline regression
// fixture for eshu-hq/eshu#5332's overcorrection guard: a genuinely
// recursive Dart function must still materialize a real self-loop CALLS
// edge (caller and callee are the same entity) through the production
// parser -> reducer.ExtractCodeCallRows path, not a hand-built envelope. The
// legacy byte-scanner this fixture guards against would have ALSO produced
// this self-loop, but for the wrong reason (every declaration matched, not
// just the genuine recursive call site); TestDeclarationOnlyDartSourceHasNoCallGraphEdges
// below is the fixture that isolates the actual bug.
func dartSelfLoopRecursionCallGraphFixture() goldenCallGraphFixture {
	return goldenCallGraphFixture{
		language: "dart_self_loop_recursion",
		files: map[string]string{
			"lib/fib.dart": `
int fib(int n) => n < 2 ? n : fib(n - 1) + fib(n - 2);
`,
		},
		caller: "fib",
		callee: "fib",
		method: codeprovenance.MethodSameFile,
	}
}

// TestDeclarationOnlyDartSourceHasNoCallGraphEdges is the real-pipeline
// regression test for eshu-hq/eshu#5332: a byte-scanner previously flagged
// any identifier immediately followed by "(" as a call, so every
// function/method/constructor declaration materialized a spurious self-loop
// CALLS edge. This drives a declaration-only Dart file through the actual
// production `parser.DefaultEngine()` -> `reducer.ExtractCodeCallRows` path
// (the same harness `TestGoldenCallGraphCorrectnessHarness` uses for every
// other language's exact-edge fixture) and asserts zero function_calls-derived
// CALLS edges reach the graph. A regression back to declaration-as-call
// would turn every entry below into a self-loop edge.
func TestDeclarationOnlyDartSourceHasNoCallGraphEdges(t *testing.T) {
	t.Parallel()

	fixture := goldenCallGraphFixture{
		language: "dart_declaration_only",
		files: map[string]string{
			"lib/widget.dart": `
class Widget {
  Widget();
  Widget.named();
  int get value => 0;
  set value(int v) {}
  void run() {}
}
void topFn() {}
`,
		},
		// caller/callee are unused by this test (no edge is expected), but
		// filled in so the fixture stays constructible with the shared type.
		caller: "run",
		callee: "run",
	}

	observed, _ := observeSourceCallGraph(t, fixture)
	if len(observed.Edges) != 0 {
		t.Fatalf("declaration-only Dart source produced %d CALLS edge(s), want 0: %#v", len(observed.Edges), observed.Edges)
	}
}

// TestDartRecursionCallGraphSelfLoopSurvives proves the fix does not
// overcorrect into a self-loop guard: driven through the same real
// parser -> reducer.ExtractCodeCallRows pipeline, a genuinely recursive Dart
// function (caller calls itself from within its own body) must still
// produce a real self-loop CALLS edge with resolution_method "same_file".
// Filtering a resolved self-loop would trade one accuracy bug (#5332's
// declaration-as-call) for its inverse (dropping real recursion) — see
// go/internal/reducer/code_call_materialization_extract.go's
// recordCodeCallSelfLoopWritten for the companion observe-only telemetry
// that watches for this class of regression in production.
func TestDartRecursionCallGraphSelfLoopSurvives(t *testing.T) {
	t.Parallel()

	fixture := dartSelfLoopRecursionCallGraphFixture()
	expected := goldenCallGraphForFixture(fixture)
	observed, methods := observeSourceCallGraph(t, fixture)

	if len(observed.Edges) != 1 {
		t.Fatalf("len(observed.Edges) = %d, want 1 self-loop edge: %#v", len(observed.Edges), observed.Edges)
	}
	edge := observed.Edges[0]
	if edge.SourceID != edge.TargetID {
		t.Fatalf("expected a self-loop edge (SourceID == TargetID), got SourceID=%q TargetID=%q", edge.SourceID, edge.TargetID)
	}
	if edge.SourceID != expected.Edges[0].SourceID {
		t.Fatalf("edge SourceID = %q, want %q", edge.SourceID, expected.Edges[0].SourceID)
	}
	assertGoldenCallGraphMethods(t, expected.Edges, codeprovenance.MethodSameFile, methods)
}
