// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"path/filepath"
	"testing"

	tree_sitter_dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// dartIndexForFixture parses one dart_comprehensive fixture through the real
// dartSourceAndSyntax path and returns the populated dartSyntaxIndex, so a test
// can assert on both the call side (index.calls) and the declaration side
// (index.functions / index.variables) of the same single traversal.
func dartIndexForFixture(t *testing.T, name string) dartSyntaxIndex {
	t.Helper()

	repoRoot := dartRepoRootForTest(t)
	path := filepath.Join(repoRoot, "tests", "fixtures", "ecosystems", "dart_comprehensive", name)

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_dart.Language())); err != nil {
		t.Fatalf("set language: %v", err)
	}
	defer parser.Close()

	_, index, err := dartSourceAndSyntax(path, parser)
	if err != nil {
		t.Fatalf("dartSourceAndSyntax(%q) error = %v", path, err)
	}
	return index
}

// callLinesFor returns the sorted source lines at which callee `name` was
// extracted (one entry per raw call site, before Parse()'s full_name dedup).
func callLinesFor(calls []dartCallSite, name string) []int {
	var lines []int
	for _, call := range calls {
		if call.name == name {
			lines = append(lines, call.line)
		}
	}
	return lines
}

func assertCallLines(t *testing.T, calls []dartCallSite, name string, want []int) {
	t.Helper()

	got := callLinesFor(calls, name)
	if len(got) != len(want) {
		t.Fatalf("call %q line count = %d %v, want %d %v (all=%#v)", name, len(got), got, len(want), want, calls)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call %q lines = %v, want %v", name, got, want)
		}
	}
}

func assertFunctionSpanPresent(t *testing.T, functions []dartFunctionSpan, name string) {
	t.Helper()

	for _, fn := range functions {
		if fn.name == name {
			return
		}
	}
	t.Fatalf("functions missing %q in %#v", name, functions)
}

func assertVariableSpanAbsent(t *testing.T, variables []dartNamedSpan, name string) {
	t.Helper()

	for _, variable := range variables {
		if variable.name == name {
			t.Fatalf("variables contains spurious %q in %#v", name, variables)
		}
	}
}

// TestParameterDefaultCallSitesAreExtracted is the #5350 gap regression guard.
// All five call sites in parameter_defaults.dart live inside subtrees that
// dartSyntaxIndex.collect prunes at the signature early-return
// (method/constructor/function signatures): optional-positional and named
// parameter default values, and a parameter annotation's argument list. The
// two-pass walk finds them because walkDartCallSites descends every named
// child; the single-pass fold must preserve that via calls-only descent into
// signature children. A naive fold that stops descending at signatures turns
// this red.
func TestParameterDefaultCallSitesAreExtracted(t *testing.T) {
	t.Parallel()

	index := dartIndexForFixture(t, "parameter_defaults.dart")

	// compute() in three default-value positions: positionalDefault (line 4),
	// the Service constructor's retries default (line 8), run's depth default
	// (line 9).
	assertCallLines(t, index.calls, "compute", []int{4, 8, 9})
	// const SizedBox() as a named-parameter default (line 5).
	assertCallLines(t, index.calls, "SizedBox", []int{5})
	// @Tag('p') parameter annotation invocation (line 6).
	assertCallLines(t, index.calls, "Tag", []int{6})

	if got, want := len(index.calls), 5; got != want {
		t.Fatalf("len(index.calls) = %d, want %d (%#v)", got, want, index.calls)
	}
}

// TestParameterDefaultDeclarationsHaveNoSpuriousParamVariables pins the
// declaration side of the same traversal. The functions wrapping the pruned
// signatures must still be extracted, and NO parameter name may leak into
// index.variables. A plain fall-through fold (removing the signature returns
// without calls-only descent) would expose collect's declaration switch to
// formal_parameter_list node kinds and emit these as spurious variable rows;
// this test fails if that happens. It also guards "both walks agree on a wrong
// answer" for the call side by asserting the exact declaration truth.
func TestParameterDefaultDeclarationsHaveNoSpuriousParamVariables(t *testing.T) {
	t.Parallel()

	index := dartIndexForFixture(t, "parameter_defaults.dart")

	for _, name := range []string{"positionalDefault", "namedDefault", "annotatedParam", "Service", "run"} {
		assertFunctionSpanPresent(t, index.functions, name)
	}
	for _, name := range []string{"x", "box", "value", "retries", "depth"} {
		assertVariableSpanAbsent(t, index.variables, name)
	}
}
