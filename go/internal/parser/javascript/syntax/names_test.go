// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

// Characterization tests for staticComputedMemberNameRe, one of the three
// within-string-content regex exceptions documented as permanent in
// AGENTS.md. Each test pins the current accepted and rejected behaviour so
// any unintended change is caught immediately.
//
// These tests moved here from
// internal/parser/javascript/residual_regex_characterization_test.go when this
// package was extracted (issue #6771), following the regex they characterize.
// The other two exceptions, javaScriptAWSClientServiceRe and
// javaScriptGCPServiceRe, stayed with semantics_ast.go in the parent package.
//
// The regex runs only against a string value the AST has already isolated; it
// is a content-classification helper, not a primary symbol-extraction scanner.
// The genuine symbol extraction runs on tree-sitter AST nodes.

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// parseRootForTest parses a JavaScript source snippet into a root node and
// source bytes. The caller must invoke the returned close function.
func parseRootForTest(t *testing.T, source string) (*tree_sitter.Node, []byte, func()) {
	t.Helper()
	language := tree_sitter.NewLanguage(tree_sitter_javascript.Language())
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	bytes := []byte(source)
	tree := parser.Parse(bytes, nil)
	if tree == nil {
		parser.Close()
		t.Fatalf("Parse() returned nil tree")
	}
	return tree.RootNode(), bytes, func() {
		tree.Close()
		parser.Close()
	}
}

// ---------------------------------------------------------------------------
// staticComputedMemberNameRe — computed-property validation helper
// ---------------------------------------------------------------------------
//
// This regex is a within-string-content shape validator. It runs only against
// the inner text of a computed-property bracket expression that the AST has
// already isolated. It accepts simple identifiers, dotted member chains, and
// decimal integer literals. It rejects dynamic expressions, template literals
// with substitutions, and anything that cannot be a static property name.

func TestJavaScriptStaticComputedMemberNameReAcceptsSimpleIdentifier(t *testing.T) {
	t.Parallel()

	cases := []string{
		"foo",
		"_bar",
		"$baz",
		"foo123",
		"FOO",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if !staticComputedMemberNameRe.MatchString(input) {
				t.Errorf("staticComputedMemberNameRe.MatchString(%q) = false, want true", input)
			}
		})
	}
}

func TestJavaScriptStaticComputedMemberNameReAcceptsDottedChain(t *testing.T) {
	t.Parallel()

	cases := []string{
		"foo.bar",
		"foo.bar.baz",
		"$foo._bar",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if !staticComputedMemberNameRe.MatchString(input) {
				t.Errorf("staticComputedMemberNameRe.MatchString(%q) = false, want true", input)
			}
		})
	}
}

func TestJavaScriptStaticComputedMemberNameReAcceptsDecimalInteger(t *testing.T) {
	t.Parallel()

	cases := []string{
		"0",
		"1",
		"42",
		"100",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if !staticComputedMemberNameRe.MatchString(input) {
				t.Errorf("staticComputedMemberNameRe.MatchString(%q) = false, want true", input)
			}
		})
	}
}

func TestJavaScriptStaticComputedMemberNameReRejectsDynamicAndInvalidForms(t *testing.T) {
	t.Parallel()

	cases := []string{
		"foo + bar", // binary expression
		"foo[bar]",  // nested bracket
		"foo()",     // call
		"${foo}",    // template substitution fragment
		"01",        // leading zero (octal-style, not a decimal integer)
		"foo bar",   // space in name
		"",          // empty string
		"foo-bar",   // hyphen
		"foo.bar.",  // trailing dot
		".foo",      // leading dot
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if staticComputedMemberNameRe.MatchString(input) {
				t.Errorf("staticComputedMemberNameRe.MatchString(%q) = true, want false", input)
			}
		})
	}
}

// firstComputedPropertyNameNode walks the AST and returns the first
// computed_property_name node, the node kind that the production wrapper
// computedPropertyName is actually called for. Bracketed class-method and
// object-literal keys (`["foo"]() {}`, `{ ["bar"]: 1 }`) produce this node; a
// subscript read (`obj["foo"]`) does not, so the test must build one of the
// former so it exercises the documented wrapper path.
func firstComputedPropertyNameNode(root *tree_sitter.Node) *tree_sitter.Node {
	var found *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if found == nil && node.Kind() == "computed_property_name" {
			found = node
		}
	})
	return found
}

// TestJavaScriptComputedPropertyNameWrapperOverRealNode pins the production
// helper computedPropertyName against real computed_property_name nodes. This
// covers the documented wrapper path end to end: the static string/number
// cases resolved by staticComputedPropertyName, the dotted-member-chain case
// validated by staticComputedMemberNameRe, and the dynamic cases the helper
// must reject. A regression in computedPropertyName (including the residual
// regex) fails this test.
func TestJavaScriptComputedPropertyNameWrapperOverRealNode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   string
	}{
		// String-literal key — resolved by the "string" case before the regex.
		{"class method string key", `class C { ["foo"]() {} }`, "foo"},
		{"object literal string key", `const o = { ["bar"]: 1 };`, "bar"},
		// Static binary concatenation — resolved by the "binary_expression" case.
		{"object literal concat key", `const o = { ["a" + "b"]: 1 };`, "ab"},
		// Numeric-literal key — resolved by the "number" case.
		{"object literal numeric key", `const o = { [42]: 1 };`, "42"},
		// Dotted member chain — the inner text is NOT resolved by
		// staticComputedPropertyName, so the wrapper falls through to
		// staticComputedMemberNameRe, which accepts the chain. This is the
		// branch that exercises the residual regex.
		{"class method dotted member key", `class C { [Symbol.iterator]() {} }`, "Symbol.iterator"},
		// Dynamic call — rejected by both the static resolver and the regex.
		{"class method dynamic call key", `class C { [getName()]() {} }`, ""},
		// Template substitution — rejected.
		{"object literal template key", "const o = { [`x${y}`]: 1 };", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, src, closeFn := parseRootForTest(t, tc.source)
			defer closeFn()

			node := firstComputedPropertyNameNode(root)
			if node == nil {
				t.Fatalf("no computed_property_name node found in %q", tc.source)
			}
			if got := computedPropertyName(node, src); got != tc.want {
				t.Fatalf("computedPropertyName() = %q, want %q (source %q)", got, tc.want, tc.source)
			}
		})
	}
}
