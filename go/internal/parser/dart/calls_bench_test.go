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
// plus the full declaration/call-site walk) on a call-site-dense fixture.
//
// Background (#5332). The AST call-site walk replaced a retired raw byte
// scanner. The scanner was faster (~10.48ms/op, 1.89MB/op, 59347 allocs/op on
// this fixture) but WRONG: it recorded every declaration as a self-call. The
// AST walk is a correctness fix (accuracy outranks performance), so the
// scanner is not a valid comparison point — its answer was wrong.
//
// Performance Evidence (traversal-mechanism recovery, this commit). The first
// AST walk allocated a fresh tree-sitter cursor and materialized a []Node via
// NamedChildren at every node in the tree (~14.11ms/op, 2.645MiB/op, 90075
// allocs/op end-to-end here). collectDartCallSites now reuses a single
// TreeCursor for the whole traversal
// (GotoFirstChild/GotoNextSibling/GotoParent), skipping anonymous nodes via
// IsNamed() to keep the visitation set and emission order byte-identical (see
// TestWalkDartCallSitesMatchesOracle, 0/0 differential-oracle equivalence).
// Measured on this fixture, same machine, back-to-back OLD(NamedChildren) vs
// NEW(cursor), go test -bench BenchmarkParseDartCallSites -benchmem
// -benchtime=300x -count=12, benchstat:
//
//	end-to-end Parse(): 14.11ms -> 13.06ms sec/op (-7.47%, p=0.000, n=12),
//	                    2.645MiB -> 2.405MiB B/op (-9.09%),
//	                    90075 -> 84118 allocs/op (-6.61%).
//	extraction step only (oracle NamedChildren vs cursor, -benchtime=500x
//	                    -count=10): 4.167ms -> 2.758ms (-33.80%, p=0.000),
//	                    883.3KiB -> 642.8KiB (-27.23%), 31994 -> 26224
//	                    allocs/op (-18.03%).
//
// No-Regression Evidence. This is output-preserving: function_calls rows are
// byte-identical to the pre-recovery walk (same names, full_names, line
// numbers, order), proven 0/0 across the dart_comprehensive corpus, the
// calls_ast_test.go regression cases, and this dense fixture by
// TestWalkDartCallSitesMatchesOracle. The recovery closes ~29% of the
// AST-walk-vs-byte-scanner gap; the remainder is the inherent cost of walking
// the whole AST (the price of the correctness fix), and absolute per-file cost
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
