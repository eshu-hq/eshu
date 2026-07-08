// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// phpParseArrayElementPairs parses src and returns every pair of
// array_element_initializer nodes under a 2-element array_creation_expression,
// mirroring exactly what phpClassMethodArray's caller (collectPHPLiteralRouteTarget,
// invoked from parser.go's "array_creation_expression" case) observes.
func phpParseArrayElementPairs(t *testing.T, src []byte) [][2]*tree_sitter.Node {
	t.Helper()
	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_php.LanguagePHP())); err != nil {
		t.Fatalf("SetLanguage(php) error = %v, want nil", err)
	}
	tree := parser.Parse(src, nil)
	t.Cleanup(tree.Close)

	var pairs [][2]*tree_sitter.Node
	shared := tree.RootNode()
	walkForArrayCreation(shared, &pairs)
	return pairs
}

func walkForArrayCreation(node *tree_sitter.Node, out *[][2]*tree_sitter.Node) {
	if node.Kind() == "array_creation_expression" {
		cursor := node.Walk()
		elements := make([]*tree_sitter.Node, 0, 2)
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if child.Kind() == "array_element_initializer" {
				elements = append(elements, cloneNodeForTest(&child))
			}
		}
		cursor.Close()
		if len(elements) == 2 {
			*out = append(*out, [2]*tree_sitter.Node{elements[0], elements[1]})
		}
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		walkForArrayCreation(&child, out)
	}
}

func cloneNodeForTest(n *tree_sitter.Node) *tree_sitter.Node {
	clone := *n
	return &clone
}

// TestPHPClassMethodArrayHandlesWrappedClassConstant locks in the equivalence
// proof for #4844: phpClassConstantClassName/phpStringLiteralValue must find
// the first ::class / string literal in the array element's full subtree, in
// the same pre-order shared.WalkNamed used to, even when it's one level below
// the array_element_initializer's direct child (parenthesized, cast, or a
// ternary's branches). The existing fixture corpus (tests/fixtures) only
// exercises the direct-child shape (`[Controller::class, 'action']`), so this
// test covers the deeper-nesting grammar positions the corpus does not.
func TestPHPClassMethodArrayHandlesWrappedClassConstant(t *testing.T) {
	src := []byte(`<?php
class Foo {}
class Bar {}
function main(): void {
  $direct = [Foo::class, 'plain'];
  $parenthesized = [(Foo::class), 'wrapped'];
  $ternary = [$flag ? Foo::class : Bar::class, 'ternary'];
  $cast = [(string) Foo::class, 'cast'];
}
`)
	pairs := phpParseArrayElementPairs(t, src)
	want := []struct {
		className  string
		methodName string
	}{
		{className: "Foo", methodName: "plain"},
		{className: "Foo", methodName: "wrapped"},
		{className: "Foo", methodName: "ternary"},
		{className: "Foo", methodName: "cast"},
	}
	if len(pairs) != len(want) {
		t.Fatalf("phpParseArrayElementPairs() = %d pairs, want %d", len(pairs), len(want))
	}
	for i, pair := range pairs {
		gotClass := phpClassConstantClassName(pair[0], src)
		gotMethod := phpStringLiteralValue(pair[1], src)
		if gotClass != want[i].className || gotMethod != want[i].methodName {
			t.Fatalf(
				"pair %d: phpClassConstantClassName/phpStringLiteralValue = (%q, %q), want (%q, %q)",
				i, gotClass, gotMethod, want[i].className, want[i].methodName,
			)
		}
	}
}
