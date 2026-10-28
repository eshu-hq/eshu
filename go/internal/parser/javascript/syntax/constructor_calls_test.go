// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

const constructorFixture = `class Service {}
class Helper {}
function makeService(): Service {
  return new Service();
}
const svc = new Service();
const helper = new Helper();
const made = makeService();
const plain = compute();
const typed: Service = makeService();
class Consumer {
  private dep: Helper;
  constructor(private readonly repo: Helper) {}
}
`

func parseConstructorFixture(t *testing.T) (*tree_sitter.Node, []byte, func()) {
	t.Helper()
	return parseTSRootForTest(t, constructorFixture)
}

func TestCollectNewExpressionVariableTypeRecordsConstructors(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseConstructorFixture(t)
	defer closeFn()
	returnTypes := FunctionReturnTypes(root, source)
	dst := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		CollectNewExpressionVariableType(node, source, returnTypes, dst)
	})

	for variable, want := range map[string]string{
		"svc": "Service", "helper": "Helper", "made": "Service", "typed": "Service",
	} {
		if dst[variable] != want {
			t.Errorf("variable type %s = %q, want %q (all: %v)", variable, dst[variable], want, dst)
		}
	}
	if _, ok := dst["plain"]; ok {
		t.Errorf("untyped call value recorded: %v", dst)
	}
	// Class fields and constructor parameter properties record this.* too.
	for _, want := range []string{"dep", "repo", "this.dep", "this.repo"} {
		if dst[want] != "Helper" {
			t.Errorf("field binding %s = %q, want Helper (all: %v)", want, dst[want], dst)
		}
	}
}

func TestCollectNewExpressionVariableTypeIgnoresOtherNodes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseConstructorFixture(t)
	defer closeFn()
	dst := map[string]string{"keep": "Kept"}
	CollectNewExpressionVariableType(root, source, nil, dst)
	if !reflect.DeepEqual(dst, map[string]string{"keep": "Kept"}) {
		t.Errorf("program node mutated dst: %v", dst)
	}
}

func TestTypedBindingNameReadsAnnotations(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTSRootForTest(t, "class C {\n  private dep: Helper;\n  constructor(private readonly repo: Helper, plain: number) {}\n}\n")
	defer closeFn()

	names := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "public_field_definition", "field_definition", "required_parameter", "optional_parameter", "formal_parameter":
			names[shared.NodeText(node, source)] = TypedBindingName(node, source)
		}
	})
	found := map[string]bool{}
	for _, name := range names {
		found[name] = true
	}
	for _, want := range []string{"dep", "repo", "plain"} {
		if !found[want] {
			t.Errorf("binding names = %v, want %q", names, want)
		}
	}
	if got := TypedBindingName(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestFunctionReturnTypesMapsAnnotatedDeclarations(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseConstructorFixture(t)
	defer closeFn()

	got := FunctionReturnTypes(root, source)
	if got["makeService"] != "Service" {
		t.Errorf("return types = %v, want makeService -> Service", got)
	}
	if _, ok := got["plain"]; ok {
		t.Errorf("return types = %v, want no unannotated entries", got)
	}
}

func TestCallInferredObjectTypeMatchesWholeReceiver(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "svc.start();\nsvc.worker.stop();\nplain();\n")
	defer closeFn()
	types := map[string]string{"svc": "Service"}

	calls := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		calls[shared.NodeText(fn, source)] = CallInferredObjectType(fn, source, types)
	})
	if calls["svc.start"] != "Service" {
		t.Errorf("inferred types = %v, want svc.start -> Service", calls)
	}
	// A compound receiver resolves only under its exact recorded text.
	if calls["svc.worker.stop"] != "" {
		t.Errorf("compound receiver resolved to %q, want empty", calls["svc.worker.stop"])
	}
	if got := CallInferredObjectType(nil, source, types); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
	if got := CallInferredObjectType(callsNode(t, root), source, nil); got != "" {
		t.Errorf("empty types = %q, want empty", got)
	}
}

func callsNode(t *testing.T, root *tree_sitter.Node) *tree_sitter.Node {
	t.Helper()
	var found *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if found == nil && node.Kind() == "call_expression" {
			found = node.ChildByFieldName("function")
		}
	})
	if found == nil {
		t.Fatal("no call function in fixture")
	}
	return found
}

func TestDeclaredTypeNameReadsAnnotations(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTSRootForTest(t, "function f(x: number): string { return \"\"; }\nconst v = 1;\n")
	defer closeFn()

	types := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "variable_declarator":
			name := node.ChildByFieldName("name")
			types[shared.NodeText(name, source)] = DeclaredTypeName(node, source)
		}
	})
	if types["f"] != "string" {
		t.Errorf("declared types = %v, want f -> string", types)
	}
	if types["v"] != "" {
		t.Errorf("declared types = %v, want v empty", types)
	}
	if got := DeclaredTypeName(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestNewExpressionConstructorNameShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "const a = new Service();\nconst b = new pkg.Thing();\nconst c = make();\n")
	defer closeFn()

	results := map[string][2]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		short, full := NewExpressionConstructorName(node.ChildByFieldName("value"), source)
		results[shared.NodeText(name, source)] = [2]string{short, full}
	})
	if results["a"] != [2]string{"Service", "Service"} {
		t.Errorf("constructor a = %v, want [Service Service]", results["a"])
	}
	if results["b"] != [2]string{"Thing", "pkg.Thing"} {
		t.Errorf("constructor b = %v, want [Thing pkg.Thing]", results["b"])
	}
	if results["c"] != [2]string{"", ""} {
		t.Errorf("non-new value = %v, want empty", results["c"])
	}
	if short, full := NewExpressionConstructorName(nil, source); short != "" || full != "" {
		t.Errorf("nil node = (%q, %q), want empty", short, full)
	}
}

func TestNewExpressionConstructorFromTextTable(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]string{
		"new Service()":             "Service",
		"new pkg.Thing(1)":          "pkg.Thing",
		"new Box<T>()":              "Box",
		"new   Spaced  ()":          "Spaced",
		"":                          "",
		"new ":                      "new",
		"Service()":                 "Service",
		"new Map<string, number>()": "Map",
	} {
		if got := newExpressionConstructorFromText(text); got != want {
			t.Errorf("constructor from %q = %q, want %q", text, got, want)
		}
	}
	for text, want := range map[string]string{
		"pkg.Thing": "Thing", "Thing": "Thing", "": "", "  ": "",
		"a.b.c": "c",
	} {
		if got := trailingConstructorName(text); got != want {
			t.Errorf("trailing name of %q = %q, want %q", text, got, want)
		}
	}
}
