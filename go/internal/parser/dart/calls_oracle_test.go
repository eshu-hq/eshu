// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

// Differential-oracle equivalence guard for Dart call-site extraction.
// oracleWalkDartCallSites is a frozen full-tree recursive NamedChildren-based
// walk, kept permanently as a test-only equivalence oracle rather than a
// one-shot golden fixture. It originally guarded the #5332 traversal-mechanism
// recovery; #5350 folded call-site detection into the single
// dartSyntaxIndex.collect traversal, and this same frozen oracle now proves the
// merged pass's index.calls stays byte-identical to the independent full-tree
// walk (TestDartCallSitesMatchOracle). The fold changed traversal ownership,
// not call-site semantics, so the oracle is unchanged.
//
// This file is test-only (frozen for equivalence, not for feature growth):
// if a real call-site semantics change is needed, update both
// dartCallChain.observe in calls.go AND this oracle together so the two
// traversal mechanisms keep agreeing on the same call-site semantics.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tree_sitter_dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// oracleCollectDartCallSites is the frozen pre-recovery entry point.
func oracleCollectDartCallSites(root *tree_sitter.Node, source []byte) []dartCallSite {
	var sites []dartCallSite
	oracleWalkDartCallSites(root, source, &sites)
	return sites
}

// oracleWalkDartCallSites is a standalone full-tree walk (per-node node.Walk()
// + NamedChildren()) frozen here as the equivalence oracle. Its node-kind
// dispatch mirrors calls.go's dartCallChain.observe (state machine, emission
// order); keep the two in sync on call-site SEMANTICS even though this oracle
// deliberately keeps its own independent traversal — the whole point is that a
// separately-written full-tree walk agrees with the merged collect.
func oracleWalkDartCallSites(node *tree_sitter.Node, source []byte, sites *[]dartCallSite) {
	if node == nil {
		return
	}

	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()

	allowDotIdentifierContinuation := dartIsObjectCreationNode(node)

	var chain []string
	var chainLine int
	previousWasPrimary := false

	emit := func() {
		if len(chain) == 0 {
			return
		}
		*sites = append(*sites, dartCallSite{
			name:     chain[len(chain)-1],
			fullName: strings.Join(chain, "."),
			line:     chainLine,
		})
	}
	reset := func() {
		chain = nil
		chainLine = 0
		previousWasPrimary = false
	}
	extend := func(name string, line int) {
		chain = append(chain, name)
		chainLine = line
	}
	extendOrStart := func(name string, line int) {
		if name == "" {
			reset()
			return
		}
		if len(chain) > 0 {
			extend(name, line)
			return
		}
		chain = []string{name}
		chainLine = line
	}

	for index := range children {
		child := &children[index]
		switch child.Kind() {
		case "identifier", "type_identifier":
			text := strings.TrimSpace(shared.NodeText(child, source))
			if allowDotIdentifierContinuation && previousWasPrimary && len(chain) > 0 {
				extend(text, shared.NodeLine(child))
			} else {
				reset()
				if text != "" {
					extend(text, shared.NodeLine(child))
				}
			}
			previousWasPrimary = true
		case "super":
			reset()
			extend("super", shared.NodeLine(child))
			previousWasPrimary = true
		case "cascade_selector":
			reset()
			if name := dartSelectorIdentifier(child, source); name != "" {
				extend(name, shared.NodeLine(child))
			}
		case "unconditional_assignable_selector", "conditional_assignable_selector":
			extendOrStart(dartSelectorIdentifier(child, source), shared.NodeLine(child))
		case "selector":
			inner := dartFirstNamedChild(child)
			switch {
			case inner == nil:
				reset()
			case inner.Kind() == "unconditional_assignable_selector" || inner.Kind() == "conditional_assignable_selector":
				extendOrStart(dartSelectorIdentifier(inner, source), shared.NodeLine(inner))
			case inner.Kind() == "argument_part":
				emit()
				reset()
			case inner.Kind() == "type_arguments":
			default:
				reset()
			}
		case "argument_part", "arguments":
			emit()
			reset()
		default:
			reset()
		}

		oracleWalkDartCallSites(child, source, sites)
	}
}

// dartMergedCallSites runs the merged single-pass dartSyntaxIndex.collect and
// returns its extracted call sites (index.calls). This is the production call
// side after #5350 folded call-site detection into collect, so the oracle now
// guards the merged pass's ownership of call extraction.
func dartMergedCallSites(root *tree_sitter.Node, source []byte) []dartCallSite {
	lines := strings.Split(string(source), "\n")
	index := &dartSyntaxIndex{}
	index.collect(root, source, lines, dartTypeSpan{}, false)
	return index.calls
}

// dartCallSitesForFile parses path with the given extraction function
// (dartMergedCallSites or oracleCollectDartCallSites) and returns the
// resulting call sites.
func dartCallSitesForFile(t *testing.T, path string, extract func(*tree_sitter.Node, []byte) []dartCallSite) []dartCallSite {
	t.Helper()

	source, err := shared.ReadSource(path)
	if err != nil {
		t.Fatalf("ReadSource(%q) error = %v", path, err)
	}

	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_dart.Language())
	if err := parser.SetLanguage(language); err != nil {
		t.Fatalf("set language: %v", err)
	}
	defer parser.Close()

	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatalf("Parse(%q) returned nil tree", path)
	}
	defer tree.Close()

	return extract(tree.RootNode(), source)
}

// TestDartCallSitesMatchOracle proves 0/0 equivalence: the merged single-pass
// dartSyntaxIndex.collect (syntax_index.go, #5350) must produce byte-identical
// dartCallSite rows (same name, full_name, and line, same order) to the frozen
// full-tree oracleWalkDartCallSites, across the dart_comprehensive fixture
// corpus (including parameter_defaults.dart, whose call sites live inside the
// signature subtrees the pre-fold collect pruned), the calls_ast_test.go
// regression corpus, and the dense synthetic benchmark fixture. The oracle is
// a frozen full-tree walk: the fold changed traversal ownership, not call-site
// semantics, so the oracle output is unchanged and this test is a real gap
// closure proof, not a vacuous pass.
func TestDartCallSitesMatchOracle(t *testing.T) {
	t.Parallel()

	repoRoot := dartRepoRootForTest(t)
	fixtureDir := filepath.Join(repoRoot, "tests", "fixtures", "ecosystems", "dart_comprehensive")
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", fixtureDir, err)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".dart") {
			continue
		}
		paths = append(paths, filepath.Join(fixtureDir, entry.Name()))
	}
	if len(paths) == 0 {
		t.Fatalf("no .dart fixtures found under %q", fixtureDir)
	}

	// calls_ast_test.go's own regression corpus, inlined as source so the
	// oracle covers exactly the shapes those unit tests assert on.
	inline := map[string]string{
		"declarations_only.dart": `class Widget {
  Widget();
  Widget.named();
  int get value => 0;
  set value(int v) {}
  void run() {}
}
void topFn() {}
`,
		"fib.dart": `int fib(int n) => n < 2 ? n : fib(n - 1) + fib(n - 2);
`,
		"fact.dart": `int fact(int n) {
  if (n <= 1) {
    return 1;
  }
  return n * fact(n - 1);
}
`,
		"mutual.dart": `void a() {
  b();
}
void b() {
  a();
}
`,
		"widget.dart": `class Foo {
  void build() {}
}

void render(Foo f) {
  f.build();
}
`,
	}
	for name, source := range inline {
		paths = append(paths, writeSource(t, name, source))
	}

	paths = append(paths, benchmarkOracleFixture(t))

	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()

			got := dartCallSitesForFile(t, path, dartMergedCallSites)
			want := dartCallSitesForFile(t, path, oracleCollectDartCallSites)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("merged collect diverges from oracle for %q:\n  got:  %#v\n  want: %#v", path, got, want)
			}
		})
	}
}

// dartRepoRootForTest walks up from the working directory to the repository
// root (the directory containing go.mod's parent, i.e. the repo root that
// holds tests/fixtures), so the test works regardless of the `go test`
// invocation's working directory.
func dartRepoRootForTest(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "tests", "fixtures", "ecosystems", "dart_comprehensive")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (tests/fixtures/ecosystems/dart_comprehensive) above %q", dir)
		}
		dir = parent
	}
}

// benchmarkOracleFixture writes the same call-site-dense synthetic fixture
// used by BenchmarkParseDartCallSites (calls_bench_test.go), so the
// equivalence proof covers the exact source the recovery was measured on.
func benchmarkOracleFixture(t *testing.T) string {
	t.Helper()

	var buf strings.Builder
	buf.WriteString("class Shared {\n  void helper() {}\n}\n\n")
	for i := 0; i < 60; i++ {
		n := workerSuffix(i)
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
	return writeSource(t, "oracle_bench_worker.dart", buf.String())
}

func workerSuffix(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
