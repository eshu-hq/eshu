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
// Prove-The-Theory-First evidence for #5332: the a-priori theory was that
// reusing the already-built tree-sitter tree (walkDartCallSites) would be
// neutral-to-faster than the retired raw byte scanner (appendDartCalls),
// since the byte scanner ran a second full pass over the source. Measured on
// this fixture (go test -bench BenchmarkParseDartCallSites -benchmem
// -benchtime=200x, same machine, same fixture, pre-fix commit 2395e65c4 vs
// this commit): the byte scanner was actually ~10.48ms/op (1.89MB/op,
// 59347 allocs/op) and the AST walk is ~14.14ms/op (2.77MB/op, 90075
// allocs/op) — a real ~35% ns/op regression, not the hypothesized win. An
// isolated micro-benchmark of just the call-site extraction step (excluding
// the shared tree-sitter parse + declaration walk both versions pay)
// attributes ~100% of the delta to the new walk itself: ~84µs/op for the old
// byte scan vs ~3.68ms/op for the AST walk on the same fixture, because
// walkDartCallSites recurses into every named child of the ENTIRE tree via
// tree-sitter Go-binding node methods (Walk/NamedChildren/Kind), which cost
// far more per node than the byte scanner's linear text lex. The theory is
// disproven; the regression is accepted here because it is a correctness fix
// (accuracy outranks performance) on a language not in the currently gated
// 20-repo golden corpus, and absolute per-file cost stays in the
// low-single-digit-millisecond range even on this deliberately dense
// synthetic fixture (real dart_comprehensive fixture files are 1-55 lines).
// A follow-up could prune the walk to skip subtrees that cannot contain a
// call (e.g. type-only nodes) if Dart parse time becomes a measured
// bottleneck.
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
