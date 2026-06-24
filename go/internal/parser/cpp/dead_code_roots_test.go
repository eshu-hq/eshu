// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpp

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
)

// cppTestParser builds a caller-owned C++ tree-sitter parser for package-level
// characterization tests. The caller closes it.
func cppTestParser(t *testing.T) *tree_sitter.Parser {
	t.Helper()
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_cpp.Language())); err != nil {
		t.Fatalf("set cpp language: %v", err)
	}
	return parser
}

// firstFunctionDefinition returns the first function_definition node in src.
func firstFunctionDefinition(t *testing.T, tree *tree_sitter.Tree) *tree_sitter.Node {
	t.Helper()
	var found *tree_sitter.Node
	var walk func(node *tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if found != nil {
			return
		}
		if node.Kind() == "function_definition" {
			found = node
			return
		}
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			walk(&child)
			if found != nil {
				return
			}
		}
	}
	walk(tree.RootNode())
	if found == nil {
		t.Fatalf("no function_definition node found")
	}
	return found
}

// TestCPPQualifiedFunctionNameAndClassFromNode locks the AST extraction of the
// out-of-line qualified method name and its enclosing class/scope. It replaces
// the prior cppQualifiedFunctionPattern regex; the cases marked "regex dropped"
// are the operator and template definitions the regex could not match and that
// the AST now recovers at byte-parity with the source declarator fields.
func TestCPPQualifiedFunctionNameAndClassFromNode(t *testing.T) {
	t.Parallel()

	parser := cppTestParser(t)
	defer parser.Close()

	cases := []struct {
		name      string
		src       string
		wantName  string
		wantClass string
	}{
		{name: "simple_method", src: "void Widget::draw() { }", wantName: "draw", wantClass: "Widget"},
		{name: "destructor", src: "Widget::~Widget() { }", wantName: "~Widget", wantClass: "Widget"},
		{name: "nested_qualifier", src: "int Outer::Inner::value() const { return 0; }", wantName: "value", wantClass: "Inner"},
		{name: "namespace_class", src: "int api::Service::run() const { return 1; }", wantName: "run", wantClass: "Service"},
		// 3+ component qualifiers nest recursively as qualified_identifier in
		// tree-sitter-cpp, so the leaf component is the function name and the
		// immediately preceding component is the class context, regardless of
		// qualifier depth (regression guard for the reviewer's mis-keying concern).
		{name: "namespace_nested_class", src: "int a::b::C::method() { return 0; }", wantName: "method", wantClass: "C"},
		{name: "namespace_deep", src: "void a::b::c::d::deep() { }", wantName: "deep", wantClass: "d"},
		{name: "operator_overload", src: "bool Vec::operator==(const Vec& o) const { return true; }", wantName: "operator==", wantClass: "Vec"},
		{name: "template_method", src: "T Box<T>::get() { return T{}; }", wantName: "get", wantClass: "Box"},
		{name: "reference_return", src: "Widget& Widget::self() { return *this; }", wantName: "self", wantClass: "Widget"},
		{name: "pointer_return", src: "Widget* Factory::make() { return nullptr; }", wantName: "make", wantClass: "Factory"},
		{name: "free_function", src: "void free_function() { }", wantName: "", wantClass: ""},
		{name: "in_class_method", src: "struct S { void m() { } };", wantName: "", wantClass: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(tc.src)
			tree := parser.Parse(source, nil)
			defer tree.Close()
			node := firstFunctionDefinition(t, tree)
			gotName, gotClass := cppQualifiedFunctionNameAndClassFromNode(node, source)
			if gotName != tc.wantName || gotClass != tc.wantClass {
				t.Fatalf("cppQualifiedFunctionNameAndClassFromNode(%q) = (%q, %q), want (%q, %q)",
					tc.src, gotName, gotClass, tc.wantName, tc.wantClass)
			}
		})
	}
}

// TestCPPQualifiedFunctionNameUsesDeclaratorNotBody confirms the AST extractor
// keys on the definition's own declarator, not on a qualified call inside the
// body. The prior regex took the last `Class::method(` match in the node text,
// so a body call like `Baz::qux()` could shadow the real `Foo::bar` name; the
// AST field walk is immune to that.
func TestCPPQualifiedFunctionNameUsesDeclaratorNotBody(t *testing.T) {
	t.Parallel()

	parser := cppTestParser(t)
	defer parser.Close()

	source := []byte("void Foo::bar() { Baz::qux(); }")
	tree := parser.Parse(source, nil)
	defer tree.Close()
	node := firstFunctionDefinition(t, tree)
	name, class := cppQualifiedFunctionNameAndClassFromNode(node, source)
	if name != "bar" || class != "Foo" {
		t.Fatalf("got (%q, %q), want (bar, Foo)", name, class)
	}
}
