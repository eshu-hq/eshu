// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// benchmarkDartFixture returns a synthetic but representative Dart source
// file: 60 classes, each declaring a constructor and two methods that call
// each other and a shared helper, matching the declaration-to-call-site
// density of a real Flutter/Dart service layer rather than a single trivial
// function. This is deliberately call-site-dense (Prove-The-Theory-First
// evidence for #5332's parser-hot-path claim: the AST call-site walk
// replaced a raw byte scan that ran a second full pass over the source).
func benchmarkDartFixture(b *testing.B) string {
	b.Helper()

	var buf strings.Builder
	buf.WriteString("class Shared {\n  void helper() {}\n}\n\n")
	for i := 0; i < 60; i++ {
		n := strconv.Itoa(i)
		buf.WriteString("class Worker" + n + " {\n")
		buf.WriteString("  final Shared shared = Shared();\n")
		buf.WriteString("  Worker" + n + "();\n")
		buf.WriteString("  void run() {\n")
		buf.WriteString("    shared.helper();\n")
		buf.WriteString("    step(1);\n")
		buf.WriteString("    step(2);\n")
		buf.WriteString("  }\n")
		buf.WriteString("  int step(int depth) {\n")
		buf.WriteString("    if (depth <= 0) {\n")
		buf.WriteString("      return 0;\n")
		buf.WriteString("    }\n")
		buf.WriteString("    return step(depth - 1) + depth;\n")
		buf.WriteString("  }\n")
		buf.WriteString("}\n\n")
	}

	// Inlined rather than reusing writeSource (helpers_test.go): that helper
	// is typed against *testing.T, and *testing.B does not satisfy it.
	path := filepath.Join(b.TempDir(), "bench_worker.dart")
	if err := os.WriteFile(path, []byte(buf.String()), 0o600); err != nil {
		b.Fatalf("write benchmark fixture: %v", err)
	}
	return path
}

// BenchmarkParseDartCallSites measures Parse() end-to-end (tree-sitter parse
// plus the merged declaration+call-site traversal) on a call-site-dense
// fixture.
//
// Background (#5332). The AST call-site walk replaced a retired raw byte
// scanner. The scanner was faster (~10.48ms/op, 1.89MB/op, 59347 allocs/op on
// this fixture) but WRONG: it recorded every declaration as a self-call. The
// AST walk is a correctness fix (accuracy outranks performance), so the
// scanner is not a valid comparison point — its answer was wrong. #5332 then
// recovered the AST-walk traversal mechanism to a single reused TreeCursor.
//
// Performance Evidence (#5350 single-pass fold, this commit). Before the fold,
// Parse() walked the tree TWICE: dartSyntaxIndex.collect for declarations and a
// separate collectDartCallSites walk for call sites (two cursor allocations and
// two NamedChildren materializations per node). The fold removes the second
// full walk by folding call detection into collect (dartCallChain.observe on
// each named child before recursing), and collect reuses one TreeCursor for the
// whole traversal (GotoFirstChild/GotoNextSibling/GotoParent, #5332 mechanism)
// instead of node.Walk()+NamedChildren per node. Measured on this fixture, same
// machine (Apple M4 Pro, 12 logical CPUs, 64 GiB), back-to-back OLD(two-pass)
// vs NEW(merged, cursor-reuse), go test -bench BenchmarkParseDartCallSites
// -benchmem -benchtime=300x -count=12, benchstat:
//
//	end-to-end Parse(): 12.92ms -> 11.08ms sec/op (-14.26%, p=0.000, n=12),
//	                    2.405MiB -> 1.793MiB B/op (-25.43%, p=0.000),
//	                    84125 -> 65614 allocs/op (-22.00%, p=0.000).
//
// The fold alone (merged pass still using per-node NamedChildren) measured
// 11.66ms (-9.75%); the cursor-reuse step added -4.99% sec/op (11.66ms ->
// 11.08ms), -11.82% B/op, -8.31% allocs on top. A prove-theory cost-floor shim
// (second walk deleted, output wrong/throwaway) measured the
// two-pass-minus-second-walk floor at ~10.35ms/op on this machine; the merged
// cursor-reuse pass lands just above that floor, as expected. An
// extraction-step-only benchmark is no longer separable after the fold (call
// detection has no standalone entry point).
//
// No-Regression Evidence. This is output-preserving: function_calls rows are
// byte-identical to the two-pass walk (same names, full_names, line numbers,
// order), proven 0/0 across the dart_comprehensive corpus (including
// parameter_defaults.dart, whose call sites live inside the signature subtrees
// the pre-fold collect pruned), the calls_ast_test.go regression cases, and
// this dense fixture by TestDartCallSitesMatchOracle. A one-shot full-index
// differential (old two-pass vs merged, DeepEqual over
// types/functions/variables/imports/calls) found 0 field mismatches across the
// corpus, so the declaration side is unchanged too. Absolute per-file cost
// stays in the low-single-digit-millisecond range even on this deliberately
// dense synthetic fixture (real dart_comprehensive files are 1-55 lines).
func BenchmarkParseDartCallSites(b *testing.B) {
	path := benchmarkDartFixture(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(path, false, shared.Options{}); err != nil {
			b.Fatalf("Parse() error = %v, want nil", err)
		}
	}
}
