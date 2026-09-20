// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fingerprint

// Regression coverage for the exact-only comment-invariance contract: on
// every exact-only tier, comment-only edits must not change the exact
// fingerprint or the token count (acceptance criterion 1 of #6835). Mirrors
// TestGoIgnoresCommentsAndRenamesPositionally per exact-only language, using
// the same node the production wiring passes to Attach (body field where the
// wiring reads ChildByFieldName("body"), the node itself where the wiring
// passes the definition node).

import (
	"strings"
	"testing"
	"unsafe"

	tree_sitter_dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
	tree_sitter_perl "github.com/alexaandru/go-sitter-forest/perl"
	tree_sitter_groovy "github.com/dekobon/tree-sitter-groovy/bindings/go"
	tree_sitter_swift "github.com/indigo-net/Brf.it/pkg/parser/treesitter/grammars/swift"
	tree_sitter_kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c_sharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_elixir "github.com/tree-sitter/tree-sitter-elixir/bindings/go"
	tree_sitter_haskell "github.com/tree-sitter/tree-sitter-haskell/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
)

type exactCommentCase struct {
	lang string
	// loader for the tree-sitter grammar.
	loader func() unsafe.Pointer
	// srcA and srcB differ only in comments.
	srcA string
	srcB string
	// find locates the node the production wiring passes to Attach.
	find func(t *testing.T, root *tree_sitter.Node, src []byte) *tree_sitter.Node
}

// findBodyField mirrors wirings that pass node.ChildByFieldName("body").
func findBodyField(kinds ...string) func(t *testing.T, root *tree_sitter.Node, src []byte) *tree_sitter.Node {
	return func(t *testing.T, root *tree_sitter.Node, src []byte) *tree_sitter.Node {
		t.Helper()
		return firstFuncBody(t, root, kinds...)
	}
}

// findSelf mirrors wirings that pass the definition node itself.
func findSelf(kinds ...string) func(t *testing.T, root *tree_sitter.Node, src []byte) *tree_sitter.Node {
	return func(t *testing.T, root *tree_sitter.Node, src []byte) *tree_sitter.Node {
		t.Helper()
		want := map[string]bool{}
		for _, k := range kinds {
			want[k] = true
		}
		var found *tree_sitter.Node
		var visit func(n *tree_sitter.Node)
		visit = func(n *tree_sitter.Node) {
			if found != nil {
				return
			}
			if want[n.Kind()] {
				found = n
				return
			}
			for i := range n.ChildCount() {
				visit(n.Child(i))
			}
		}
		visit(root)
		if found == nil {
			t.Fatalf("no %v node found", kinds)
		}
		return found
	}
}

// requireCommentLeaf guards against vacuous invariance: the fingerprinted
// node must actually contain a comment node, so equal hashes prove exclusion
// rather than absence. It matches any kind containing "comment", which covers
// every grammar's comment kinds without duplicating the production table.
func requireCommentLeaf(t *testing.T, lang string, node *tree_sitter.Node) {
	t.Helper()
	found := false
	var visit func(n *tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		if found {
			return
		}
		if strings.Contains(strings.ToLower(n.Kind()), "comment") {
			found = true
			return
		}
		for i := range n.ChildCount() {
			visit(n.Child(i))
		}
	}
	visit(node)
	if !found {
		t.Fatalf("%s: fingerprinted node contains no comment leaf; test would pass vacuously", lang)
	}
}

// findElixirDef mirrors the elixir wiring, which passes the whole `def`
// definition node (a call whose target is `def`).
func findElixirDef(t *testing.T, root *tree_sitter.Node, src []byte) *tree_sitter.Node {
	t.Helper()
	var found *tree_sitter.Node
	var visit func(n *tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		if found != nil {
			return
		}
		if n.Kind() == "call" {
			if target := n.ChildByFieldName("target"); target != nil {
				if text := target.Utf8Text(src); text == "def" {
					found = n
					return
				}
			}
		}
		for i := range n.ChildCount() {
			visit(n.Child(i))
		}
	}
	visit(root)
	if found == nil {
		t.Fatal("no def call node found")
	}
	return found
}

func TestExactOnlyTiersIgnoreComments(t *testing.T) {
	cases := []exactCommentCase{
		{
			"c", tree_sitter_c.Language,
			"int f() {\n// leading\nreturn 1;\n}\n",
			"int f() {\n// rewritten comment\nreturn 1; /* trailing */\n}\n",
			findBodyField("function_definition"),
		},
		{
			"cpp", tree_sitter_cpp.Language,
			"int f() {\n// leading\nreturn 1;\n}\n",
			"int f() {\n// rewritten comment\nreturn 1; /* trailing */\n}\n",
			findBodyField("function_definition"),
		},
		{
			// Production C# parsing passes "c_sharp" (see csharp/language.go),
			// so the invariance pin uses the production key, not the
			// grammar shorthand.
			"c_sharp", tree_sitter_c_sharp.Language,
			"class A {\nint F() {\n// leading\nreturn 1;\n}\n}\n",
			"class A {\nint F() {\n// rewritten comment\nreturn 1; /* trailing */\n}\n}\n",
			findBodyField("method_declaration"),
		},
		{
			"dart", tree_sitter_dart.Language,
			"int f() {\n// leading\nreturn 1;\n}\n",
			"int f() {\n// rewritten comment\nreturn 1; /* trailing */\n}\n",
			findSelf("function_body"),
		},
		{
			"elixir", tree_sitter_elixir.Language,
			"defmodule M do\ndef f do\n# leading\n1\nend\nend\n",
			"defmodule M do\ndef f do\n# rewritten comment\n1\nend\nend\n",
			findElixirDef,
		},
		{
			"groovy", tree_sitter_groovy.Language,
			"def f() {\n// leading\nreturn 1\n}\n",
			"def f() {\n// rewritten comment\nreturn 1 /* trailing */\n}\n",
			findBodyField("method_declaration"),
		},
		{
			"haskell", tree_sitter_haskell.Language,
			"module M where\nf :: Int\nf =\n  -- interior\n  1\n",
			"module M where\nf :: Int\nf =\n  -- rewritten interior\n  1\n",
			findSelf("bind", "function"),
		},
		{
			// tree-sitter-kotlin v1.1.0 emits /* */ as block_comment (verified
			// by parse probe, not multiline_comment), so both comment kinds
			// are exercised here to lock the invariance contract.
			"kotlin", tree_sitter_kotlin.Language,
			"fun f(): Int {\n// leading\nreturn 1 /* leading block */\n}\n",
			"fun f(): Int {\n// rewritten\nreturn 1 /* rewritten block */\n}\n",
			findSelf("function_body"),
		},
		{
			"perl", tree_sitter_perl.GetLanguage,
			"sub f {\n# leading\nreturn 1;\n}\n",
			"sub f {\n# rewritten comment\nreturn 1;\n}\n",
			findBodyField("subroutine_declaration_statement"),
		},
		{
			"php", tree_sitter_php.LanguagePHP,
			"<?php\nfunction f() {\n// leading\nreturn 1;\n}\n",
			"<?php\nfunction f() {\n// rewritten comment\nreturn 1; /* trailing */\n}\n",
			findBodyField("function_definition"),
		},
		{
			"ruby", tree_sitter_ruby.Language,
			"def f\n1\n# interior\n2\nend\n",
			"def f\n1\n# rewritten interior\n2\nend\n",
			findBodyField("method"),
		},
		{
			"rust", tree_sitter_rust.Language,
			"fn f() -> i32 {\n// leading\n1\n}\n",
			"fn f() -> i32 {\n// rewritten comment\n1 /* trailing */\n}\n",
			findBodyField("function_item"),
		},
		{
			"scala", tree_sitter_scala.Language,
			"object O {\ndef f: Int = {\n// leading\n1\n}\n}\n",
			"object O {\ndef f: Int = {\n// rewritten comment\n1 /* trailing */\n}\n}\n",
			findBodyField("function_definition"),
		},
		{
			"swift", tree_sitter_swift.Language,
			"func f() -> Int {\n// leading\nreturn 1\n}\n",
			"func f() -> Int {\n// rewritten comment\nreturn 1 /* trailing */\n}\n",
			findBodyField("function_declaration"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			fp := func(src string) Result {
				root, bs := parseOne(t, tc.loader, src)
				node := tc.find(t, root, bs)
				requireCommentLeaf(t, tc.lang, node)
				got := FingerprintBody(tc.lang, node, bs)
				return *got
			}
			a, b := fp(tc.srcA), fp(tc.srcB)
			if a.Exact != b.Exact {
				t.Fatalf("%s: comment-only edit changed fp_exact %q -> %q", tc.lang, a.Exact, b.Exact)
			}
			if a.TokenCount != b.TokenCount {
				t.Fatalf("%s: comment-only edit changed token count %d -> %d", tc.lang, a.TokenCount, b.TokenCount)
			}
			if a.RenamedSupported || a.Sketch != nil || len(a.Bands) != 0 {
				t.Fatalf("%s: must stay exact-only (no renamed/sketch/bands)", tc.lang)
			}
		})
	}
}

// TestExactStillDistinguishesCodeChange is the negative control: a real code
// edit must change fp_exact, proving the invariance test above is not vacuous.
func TestExactStillDistinguishesCodeChange(t *testing.T) {
	fp := func(body string) string {
		root, src := parseOne(t, tree_sitter_c.Language, "int f() {\n"+body+"\n}\n")
		got := FingerprintBody("c", firstFuncBody(t, root, "function_definition"), src)
		return got.Exact
	}
	if fp("return 1;") == fp("return 2;") {
		t.Fatal("c: code change must change fp_exact")
	}
}
