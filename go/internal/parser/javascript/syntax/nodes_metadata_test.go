// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestIsFunctionValueKinds(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseRootForTest(t, "const a = function() {};\nconst b = () => {};\nfunction* g() {}\nconst c = 1;\n")
	defer closeFn()

	truthy, falsy := 0, 0
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" && node.Kind() != "function_declaration" && node.Kind() != "generator_function_declaration" {
			return
		}
		value := node
		if node.Kind() == "variable_declarator" {
			value = node.ChildByFieldName("value")
		}
		if IsFunctionValue(value) {
			truthy++
		} else {
			falsy++
		}
	})
	if truthy != 3 || falsy != 1 {
		t.Errorf("function values truthy=%d falsy=%d, want 3 and 1", truthy, falsy)
	}
	if IsFunctionValue(nil) {
		t.Error("nil node reported as function value")
	}
}

func TestInsideFunctionWalksLookup(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "const top = 1;\nfunction run() {\n  const inner = () => top;\n}\n")
	defer closeFn()
	parents := BuildParentLookup(root)

	var inside, arrow, outside *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "identifier" {
			return
		}
		switch shared.NodeText(node, source) {
		case "inner":
			if inside == nil {
				inside = node
			}
		case "top":
			if outside == nil {
				outside = node
			}
		}
	})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if arrow == nil && node.Kind() == "arrow_function" {
			arrow = node
		}
	})
	if inside == nil || outside == nil || arrow == nil {
		t.Fatal("fixture did not yield all probe nodes")
	}
	// The declarator name sits inside run; the initializer identifier at
	// program scope does not; the arrow body identifier nests two levels.
	if !InsideFunction(inside, parents) {
		t.Error("declarator inside run not reported inside a function")
	}
	if InsideFunction(outside, parents) {
		t.Error("program-scope identifier reported inside a function")
	}
	if !InsideFunction(arrow, parents) {
		t.Error("arrow function inside run not reported inside a function")
	}
	_ = source
}

func TestCallNameUnwrapsShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "run();\nns.handler();\n(plain)();\n42();\n")
	defer closeFn()

	names := []string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "call_expression" {
			names = append(names, CallName(node.ChildByFieldName("function"), source))
		}
	})
	for i, want := range []string{"run", "handler", "plain", ""} {
		if i >= len(names) || names[i] != want {
			t.Errorf("call names = %v, want %v at index %d", names, want, i)
		}
	}
	if got := CallName(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestCallFullNameKeepsChain(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "  ns.handler();\n")
	defer closeFn()

	var fn *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if fn == nil && node.Kind() == "call_expression" {
			fn = node.ChildByFieldName("function")
		}
	})
	if got := CallFullName(fn, source); got != "ns.handler" {
		t.Errorf("full name = %q, want trimmed chain", got)
	}
	if got := CallFullName(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestJSXComponentNameTakesFinalSegment(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "const a = <Foo />;\nconst b = <A.B />;\n")
	defer closeFn()

	names := []string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "jsx_element" || node.Kind() == "jsx_self_closing_element" {
			names = append(names, JSXComponentName(node, source))
		}
	})
	if len(names) != 2 || names[0] != "Foo" || names[1] != "B" {
		t.Errorf("component names = %v, want [Foo B]", names)
	}
	if got := JSXComponentName(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestNodeContainsKindSelfAndDescendant(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseRootForTest(t, "function f() {}\n")
	defer closeFn()

	var funcNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if funcNode == nil && node.Kind() == "function_declaration" {
			funcNode = node
		}
	})
	if !NodeContainsKind(funcNode, "function_declaration") {
		t.Error("self kind not contained")
	}
	if !NodeContainsKind(root, "identifier") {
		t.Error("descendant identifier not contained")
	}
	if NodeContainsKind(root, "no_such_kind") {
		t.Error("missing kind reported contained")
	}
	if NodeContainsKind(nil, "identifier") {
		t.Error("nil node reported contained")
	}
}

func TestNodeSameRangeComparesBytes(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseRootForTest(t, "f(a);\n")
	defer closeFn()

	var first, second *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "identifier" {
			return
		}
		if first == nil {
			first = node
		} else if second == nil {
			second = node
		}
	})
	if !NodeSameRange(first, first) {
		t.Error("node not same-range as itself")
	}
	if NodeSameRange(first, second) {
		t.Error("distinct nodes reported same range")
	}
	if NodeSameRange(first, nil) || NodeSameRange(nil, first) {
		t.Error("nil comparison reported same range")
	}
}

func TestHasExpressImportSpellings(t *testing.T) {
	t.Parallel()

	for source, want := range map[string]bool{
		`const e = require("express");`:  true,
		`const e = require('express');`:  true,
		`import e from "express";`:       true,
		`import e from 'express';`:       true,
		`const e = require("expressx");`: false,
		`// require("express") comment`:  true, // documented: text match, comments count
		`const e = require("fastify");`:  false,
	} {
		if got := HasExpressImport(source); got != want {
			t.Errorf("HasExpressImport(%q) = %v, want %v", source, got, want)
		}
	}
}

func TestDocstringReadsJSDocBlocks(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "/** Adds one. */\nfunction add(a) { return a + 1; }\n/**\n * Multi-line.\n * Second.\n */\nfunction multi() {}\n/* plain block */\nfunction blocked() {}\nfunction bare() {}\n/** detached */\n\nfunction gapped() {}\n")
	defer closeFn()

	texts := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := node.ChildByFieldName("name")
		texts[shared.NodeText(name, source)] = Docstring(node, source)
	})

	if texts["add"] != "Adds one." {
		t.Errorf("single-line docstring = %q, want the sentence", texts["add"])
	}
	if texts["multi"] != "Multi-line.\nSecond." {
		t.Errorf("multi-line docstring = %q, want both lines", texts["multi"])
	}
	if texts["blocked"] != "" {
		t.Errorf("plain block docstring = %q, want empty (JSDoc only)", texts["blocked"])
	}
	if texts["bare"] != "" {
		t.Errorf("bare docstring = %q, want empty", texts["bare"])
	}
	// Blank lines between the comment and the node are skipped; only a
	// blank after collected comment lines breaks the association.
	if texts["gapped"] != "detached" {
		t.Errorf("detached docstring = %q, want the comment (leading blanks skipped)", texts["gapped"])
	}
	if got := Docstring(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestFunctionKindClassifiesModifiers(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "async function fetchIt() {}\nfunction* gen() {}\nfunction plain() {}\nclass C {\n  get name() { return \"x\"; }\n  set name(v) {}\n  async load() {}\n  *items() {}\n  method() {}\n}\n")
	defer closeFn()

	kinds := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "generator_function_declaration", "method_definition":
			name := node.ChildByFieldName("name")
			kinds[shared.NodeText(name, source)] = FunctionKind(node, source)
		}
	})

	for name, want := range map[string]string{
		"fetchIt": "async", "gen": "generator", "plain": "",
		"name": "", // get/set share one map key; asserted separately below
		"load": "async", "items": "generator", "method": "",
	} {
		if name == "name" {
			continue
		}
		if kinds[name] != want {
			t.Errorf("function kind %s = %q, want %q (all: %v)", name, kinds[name], want, kinds)
		}
	}
	getter, setter := 0, 0
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_definition" {
			return
		}
		switch FunctionKind(node, source) {
		case "getter":
			getter++
		case "setter":
			setter++
		}
	})
	if getter != 1 || setter != 1 {
		t.Errorf("getters=%d setters=%d, want 1 each", getter, setter)
	}
	if got := FunctionKind(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
	var ident *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	if got := FunctionKind(ident, source); got != "" {
		t.Errorf("identifier kind = %q, want empty", got)
	}
}
