// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// TestImportPathForAliasDuplicateAliasIsDeterministic pins issue #6947: two
// import paths sharing one alias (legal in uncompilable or intermediate
// sources, e.g. two packages whose base name is "taint") must resolve to one
// stable path. ImportPathForAlias used to return the first match of a Go map
// range, so the winner was random per process and flipped stable_symbol_key
// rows run to run.
func TestImportPathForAliasDuplicateAliasIsDeterministic(t *testing.T) {
	t.Parallel()

	index := map[string][]string{
		"github.com/eshu-hq/eshu/go/internal/storage/postgres/code/taint": {"taint"},
		"github.com/eshu-hq/eshu/go/internal/reducer/code/taint":          {"taint"},
	}
	const want = "github.com/eshu-hq/eshu/go/internal/reducer/code/taint"

	first := ImportPathForAlias("taint", index)
	if first != want {
		t.Fatalf("ImportPathForAlias(taint) = %q, want lexicographically smallest %q", first, want)
	}
	for i := 0; i < 200; i++ {
		if got := ImportPathForAlias("taint", index); got != want {
			t.Fatalf("iteration %d: ImportPathForAlias(taint) = %q, want stable %q", i, got, want)
		}
	}
}

// TestQualifiedCallFunctionNameDuplicateAliasIsDeterministic pins issue
// #6947 one level up from ImportPathForAlias: a selector call on an alias
// bound by two import paths must qualify to the same (lexicographically
// smallest) path on every resolution.
func TestQualifiedCallFunctionNameDuplicateAliasIsDeterministic(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nimport (\n\t\"example.com/a/taint\"\n\t\"example.com/b/taint\"\n)\n\nfunc f() {\n\ttaint.Evidence()\n}\n")
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v", err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree")
	}
	defer tree.Close()

	var callNode *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if callNode == nil && node.Kind() == "call_expression" {
			callNode = node
		}
	})
	if callNode == nil {
		t.Fatal("no call_expression found in fixture source")
	}

	index := map[string][]string{
		"example.com/b/taint": {"taint"},
		"example.com/a/taint": {"taint"},
	}
	const want = "example.com/a/taint.evidence"
	for i := 0; i < 100; i++ {
		if got := QualifiedCallFunctionName(callNode, source, index); got != want {
			t.Fatalf("iteration %d: QualifiedCallFunctionName = %q, want stable %q", i, got, want)
		}
	}
}

// TestImportPathForAliasSingleAndMissing pins the unchanged contract: a
// unique alias resolves to its path, unknown aliases resolve to "".
func TestImportPathForAliasSingleAndMissing(t *testing.T) {
	t.Parallel()

	index := map[string][]string{
		"example.com/foo/bar": {"bar"},
	}
	if got := ImportPathForAlias("bar", index); got != "example.com/foo/bar" {
		t.Fatalf("ImportPathForAlias(bar) = %q, want %q", got, "example.com/foo/bar")
	}
	if got := ImportPathForAlias("  bar  ", index); got != "example.com/foo/bar" {
		t.Fatalf("ImportPathForAlias(padded bar) = %q, want trimmed match", got)
	}
	if got := ImportPathForAlias("missing", index); got != "" {
		t.Fatalf("ImportPathForAlias(missing) = %q, want %q", got, "")
	}
	if got := ImportPathForAlias("bar", nil); got != "" {
		t.Fatalf("ImportPathForAlias on nil index = %q, want %q", got, "")
	}
}
