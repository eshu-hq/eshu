// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestMemberBaseAndPropertySplitsSelector(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "app.get(\"/x\", h);\nplain();\n")
	defer closeFn()

	results := map[string][3]any{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		base, property, ok := MemberBaseAndProperty(fn, source)
		results[shared.NodeText(fn, source)] = [3]any{base, property, ok}
	})
	if results["app.get"] != [3]any{"app", "get", true} {
		t.Errorf("member split = %v, want [app get true]", results["app.get"])
	}
	if results["plain"] != [3]any{"", "", false} {
		t.Errorf("identifier split = %v, want empty", results["plain"])
	}
	if _, _, ok := MemberBaseAndProperty(nil, source); ok {
		t.Error("nil node split ok")
	}
}

func TestMemberBaseUnwrapsRouteChain(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "app.route(\"/x\").get(handler);\napp.get(\"/y\", other);\n")
	defer closeFn()

	chains := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		if fn == nil || fn.Kind() != "member_expression" {
			return
		}
		base, _, ok := MemberBaseAndProperty(fn, source)
		chains[shared.NodeText(fn, source)] = ok && base == "app" && IsExpressRouteChain(fn, source)
	})
	if !chains["app.route(\"/x\").get"] {
		t.Errorf("route chain not recognized: %v", chains)
	}
	if chains["app.get"] {
		t.Errorf("plain member call reported as route chain: %v", chains)
	}
	if IsExpressRouteChain(nil, source) {
		t.Error("nil node reported as route chain")
	}
}

func TestExpressHandlerNamesCollectsNamedCallbacks(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "app.get(\"/x\", auth, handler);\napp.post(\"/y\", [first, second, first]);\napp.put(\"/z\", (req, res) => {});\n")
	defer closeFn()

	var calls []*tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "call_expression" {
			calls = append(calls, node)
		}
	})
	if len(calls) != 3 {
		t.Fatalf("found %d calls, want 3", len(calls))
	}
	args := func(call *tree_sitter.Node, index int) *tree_sitter.Node {
		arguments := call.ChildByFieldName("arguments")
		cursor := arguments.Walk()
		defer cursor.Close()
		return &arguments.NamedChildren(cursor)[index]
	}
	first := ExpressHandlerNames(args(calls[0], 1), source)
	second := ExpressHandlerNames(args(calls[0], 2), source)
	if !reflect.DeepEqual(first, []string{"auth"}) || !reflect.DeepEqual(second, []string{"handler"}) {
		t.Errorf("handler args = %v %v, want [auth] [handler]", first, second)
	}
	arrayed := ExpressHandlerNames(args(calls[1], 1), source)
	if !reflect.DeepEqual(arrayed, []string{"first", "second"}) {
		t.Errorf("array handlers = %v, want deduplicated [first second]", arrayed)
	}
	if got := ExpressHandlerNames(args(calls[2], 1), source); len(got) != 0 {
		t.Errorf("inline arrow handlers = %v, want empty (no stable symbol)", got)
	}
	if got := ExpressHandlerNames(nil, source); len(got) != 0 {
		t.Errorf("nil node = %v, want empty", got)
	}
}

func TestIdentifierNameKinds(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "const o = { key: 1 };\n")
	defer closeFn()

	var ident, pair *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "identifier":
			if ident == nil {
				ident = node
			}
		case "pair":
			if pair == nil {
				pair = node
			}
		}
	})
	if got := IdentifierName(ident, source); got == "" {
		t.Error("identifier name empty")
	}
	if got := IdentifierName(pair, source); got != "" {
		t.Errorf("pair name = %q, want empty (identifiers only)", got)
	}
	if got := IdentifierName(nil, source); got != "" {
		t.Errorf("nil name = %q, want empty", got)
	}
	if got := ObjectPairKey(pair, source); got != "key" {
		t.Errorf("pair key = %q, want key", got)
	}
}

func TestObjectPairKeyStringLiterals(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "const o = { \"quoted key\": 1 };\n")
	defer closeFn()

	var pair *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if pair == nil && node.Kind() == "pair" {
			pair = node
		}
	})
	if got := ObjectPairKey(pair, source); got != "quoted key" {
		t.Errorf("string pair key = %q, want the unquoted text", got)
	}
}

func TestImplementedInterfacesReducesNames(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTSRootForTest(t, "class A implements Store {}\nclass B implements ns.Cache, Store {}\nclass C implements Box<string> {}\nclass D implements Store, Store {}\nclass E {}\n")
	defer closeFn()

	results := map[string][]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "class_declaration" {
			return
		}
		name := node.ChildByFieldName("name")
		results[shared.NodeText(name, source)] = ImplementedInterfaces(node, source)
	})
	for class, want := range map[string][]string{
		"A": {"Store"}, "B": {"Cache", "Store"}, "C": {"Box"}, "D": {"Store"},
	} {
		if !reflect.DeepEqual(results[class], want) {
			t.Errorf("class %s interfaces = %v, want %v", class, results[class], want)
		}
	}
	if len(results["E"]) != 0 {
		t.Errorf("class E interfaces = %v, want none", results["E"])
	}
}

func TestTypeParametersReadsField(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTSRootForTest(t, "function pair<T, U>(a: T, b: U): void {}\nfunction plain(a: number): void {}\n")
	defer closeFn()

	results := map[string][]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := node.ChildByFieldName("name")
		results[shared.NodeText(name, source)] = TypeParameters(node, source)
	})
	if !reflect.DeepEqual(results["pair"], []string{"T", "U"}) {
		t.Errorf("pair type params = %v, want [T U]", results["pair"])
	}
	if len(results["plain"]) != 0 {
		t.Errorf("plain type params = %v, want empty (never nil)", results["plain"])
	}
	if got := TypeParameters(nil, source); got == nil || len(got) != 0 {
		t.Errorf("nil node = %#v, want empty non-nil", got)
	}
}

func TestTypeParameterNamesSplitsTopLevel(t *testing.T) {
	t.Parallel()

	for declaration, want := range map[string][]string{
		"function f<T, U>": {"T", "U"},
		"function f<T extends Record<string, number>, U = string>": {"T", "U"},
		"function f()":                 {},
		"class C<T extends Box<U>, V>": {"T", "V"},
		"function f<T>":                {"T"},
	} {
		if got := TypeParameterNames(declaration); !reflect.DeepEqual(got, want) {
			t.Errorf("TypeParameterNames(%q) = %v, want %v", declaration, got, want)
		}
	}
}
