// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// receiverFixtureSource exercises every LocalReceiverBindings statement
// kind: typed parameters, composite-literal and constructor-return short
// declarations, a typed var_spec without a value, a non-struct short
// declaration, and a range over a map parameter.
const receiverFixtureSource = `package p

type Worker struct{}
type Client struct{}

func NewWorker() Worker { return Worker{} }

func run(m map[string]Worker, w Worker, c Client) {
	direct := Worker{}
	made := NewWorker()
	var declared Client
	plain := 42
	for k, v := range m {
		use(v, k)
	}
	useFirst(direct, made, declared, w, c, plain)
}
`

func parseReceiverFixture(t *testing.T) (*tree_sitter.Tree, []byte) {
	t.Helper()
	source := []byte(receiverFixtureSource)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	t.Cleanup(func() { tree.Close() })
	return tree, source
}

func bindingsByVariable(bindings []LocalReceiverBinding) map[string][]LocalReceiverBinding {
	byVar := make(map[string][]LocalReceiverBinding)
	for _, binding := range bindings {
		byVar[binding.Variable] = append(byVar[binding.Variable], binding)
	}
	return byVar
}

func TestLocalReceiverBindingsCoversEveryStatementKind(t *testing.T) {
	t.Parallel()

	tree, source := parseReceiverFixture(t)
	root := tree.RootNode()
	lookup := BuildParentLookup(root)
	bindings := LocalReceiverBindings(root, source, map[string]string{"NewWorker": "Worker"}, lookup)
	byVar := bindingsByVariable(bindings)

	for variable, want := range map[string]string{
		"w":      "Worker",
		"direct": "Worker",
		"made":   "Worker",
		"v":      "Worker",
	} {
		got, ok := byVar[variable]
		if !ok || len(got) == 0 {
			t.Errorf("no binding recorded for %q, want type %q", variable, want)
			continue
		}
		if got[0].typeName != want {
			t.Errorf("binding for %q has type %q, want %q", variable, got[0].typeName, want)
		}
	}

	// A typed var_spec without a value and the range key contribute no
	// receiver evidence.
	for _, variable := range []string{"declared", "k"} {
		if got, ok := byVar[variable]; ok && len(got) != 0 {
			t.Errorf("unexpected binding for %q: %#v, want none", variable, got)
		}
	}
	// A non-struct short declaration still records the variable with an
	// empty type, so inference skips it without losing the declaration.
	got, ok := byVar["plain"]
	if !ok || len(got) == 0 {
		t.Fatal("no binding recorded for plain, want one with empty type")
	}
	if got[0].typeName != "" {
		t.Errorf("binding for plain has type %q, want empty", got[0].typeName)
	}
}

func TestLocalReceiverBindingsParamInterfaceIsNotConcrete(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Iface interface{ M() }\n\nfunc run(i Iface) {\n\tuse(i)\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	lookup := BuildParentLookup(root)

	bindings := LocalReceiverBindings(root, source, nil, lookup)
	byVar := bindingsByVariable(bindings)
	got, ok := byVar["i"]
	if !ok || len(got) == 0 {
		t.Fatal("no binding recorded for interface parameter i, want one non-concrete binding")
	}
	if got[0].typeName != "Iface" {
		t.Errorf("binding for i has type %q, want %q", got[0].typeName, "Iface")
	}
	if got[0].concrete {
		t.Error("binding for interface parameter i is concrete, want non-concrete")
	}
}

func TestCollectConstructorReturnTypeRecordsOnlyTypedFunctions(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Worker struct{}\n\nfunc NewWorker() Worker { return Worker{} }\n\nfunc noResult() {}\n\nfunc blank() { _ = 1 }\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()

	returns := make(map[string]string)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "function_declaration" {
			CollectConstructorReturnType(node, source, returns)
		}
	})

	if got := returns["NewWorker"]; got != "Worker" {
		t.Errorf("returns[NewWorker] = %q, want %q", got, "Worker")
	}
	for _, name := range []string{"noResult", "blank"} {
		if got, ok := returns[name]; ok {
			t.Errorf("returns[%q] = %q, want no entry", name, got)
		}
	}
}

func TestLocalNameBindingsFromParametersScopesParamsToBody(t *testing.T) {
	t.Parallel()

	tree, source := parseReceiverFixture(t)
	root := tree.RootNode()

	var funcNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := node.ChildByFieldName("name")
		if name != nil && string(source[name.StartByte():name.EndByte()]) == "run" {
			funcNode = node
		}
	})
	if funcNode == nil {
		t.Fatal("no run function_declaration in fixture")
	}

	bindings := LocalNameBindingsFromParameters(funcNode, source)
	names := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		names = append(names, binding.variable)
	}
	for _, want := range []string{"m", "w", "c"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("parameter %q missing from bindings %v", want, names)
		}
	}
	for _, binding := range bindings {
		if binding.scopeStart > binding.scopeEnd {
			t.Errorf("binding %q has inverted scope [%d, %d]", binding.variable, binding.scopeStart, binding.scopeEnd)
		}
	}

	// A node without parameters or body yields no bindings.
	if got := LocalNameBindingsFromParameters(root, source); len(got) != 0 {
		t.Errorf("LocalNameBindingsFromParameters(root) = %#v, want empty", got)
	}
}

func TestLocalNameBindingsFromNamesNeedsEnclosingScope(t *testing.T) {
	t.Parallel()

	tree, source := parseReceiverFixture(t)
	root := tree.RootNode()
	lookup := BuildParentLookup(root)

	var shortNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if shortNode == nil && node.Kind() == "short_var_declaration" {
			shortNode = node
		}
	})
	if shortNode == nil {
		t.Fatal("no short_var_declaration in fixture")
	}
	names := IdentifierNodes(shortNode.ChildByFieldName("left"), source)
	bindings := LocalNameBindingsFromNames(shortNode, names, source, lookup)
	if len(bindings) == 0 {
		t.Fatal("LocalNameBindingsFromNames returned no bindings for short declaration names")
	}
	if bindings[0].variable != "direct" {
		t.Errorf("first binding variable = %q, want %q", bindings[0].variable, "direct")
	}

	// A package-level node has no enclosing lexical scope.
	if got := LocalNameBindingsFromNames(root, names, source, lookup); len(got) != 0 {
		t.Errorf("LocalNameBindingsFromNames(root) = %#v, want empty", got)
	}
}

func TestNewLocalReceiverBindingWithoutScopeIsZeroValue(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nvar x = f()\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	lookup := BuildParentLookup(root)

	var callNode, nameNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call_expression":
			if callNode == nil {
				callNode = node
			}
		case "identifier":
			if nameNode == nil && string(source[node.StartByte():node.EndByte()]) == "x" {
				nameNode = node
			}
		}
	})
	if callNode == nil || nameNode == nil {
		t.Fatal("fixture did not yield package-level call and name nodes")
	}

	binding := NewLocalReceiverBinding(callNode, nameNode, "Worker", true, source, lookup)
	if binding.Variable != "" {
		t.Errorf("package-level binding.Variable = %q, want zero value", binding.Variable)
	}
}
