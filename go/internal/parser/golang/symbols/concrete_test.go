// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestStructFieldConcreteTypesKeepsOnlyKnownStructFields(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Worker struct{}\n\ntype Job struct {\n\tOwner Worker\n\tCount int\n\tNick string\n}\n\ntype Alias = Worker\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	got := StructFieldConcreteTypes(tree.RootNode(), source, map[string]struct{}{"worker": {}})
	want := map[string]map[string]string{"job": {"owner": "worker"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StructFieldConcreteTypes() = %#v, want %#v", got, want)
	}
}

func TestConcreteTypeFromExpressionUnwrapsToCompositeLiteral(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Worker struct{}\n\nfunc run() {\n\ta := Worker{}\n\tb := (Worker{})\n\tc := plain\n\td := -Worker{}\n\te := 1 + 2\n\t_, _, _, _, _ = a, b, c, d, e\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	structs := map[string]struct{}{"worker": {}}

	values := map[string]string{}
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() != "short_var_declaration" {
			return
		}
		left := node.ChildByFieldName("left")
		right := node.ChildByFieldName("right")
		if left == nil || right == nil {
			return
		}
		name := string(source[left.StartByte():left.EndByte()])
		values[name] = ConcreteTypeFromExpression(right, source, structs)
	})

	// Unary expressions recurse into the operand; a binary expression is
	// not a shape this resolver unwraps.
	for variable, want := range map[string]string{"a": "worker", "b": "worker", "c": "", "d": "worker", "e": ""} {
		if got := values[variable]; got != want {
			t.Errorf("concrete type of %s = %q, want %q", variable, got, want)
		}
	}
	if got := ConcreteTypeFromExpression(nil, source, structs); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestConcreteTypeFromTypeNodeFindsFirstIdentifier(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Worker struct{}\n\nfunc use(w *Worker, m map[string]Worker, s []Worker, n int) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	structs := map[string]struct{}{"worker": {}}

	types := []string{}
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() == "parameter_declaration" {
			types = append(types, ConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structs))
		}
	})
	// The map parameter resolves empty: the walk takes the first
	// type_identifier under the map_type, which is the string key.
	if !reflect.DeepEqual(types, []string{"worker", "", "worker", ""}) {
		t.Errorf("parameter concrete types = %#v, want [worker \"\" worker \"\"]", types)
	}
	if got := ConcreteTypeFromTypeNode(nil, source, structs); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestCompositeLiteralTypeNameReadsTypeNode(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Worker struct{}\n\nvar w = Worker{}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var literal *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if literal == nil && node.Kind() == "composite_literal" {
			literal = node
		}
	})
	if literal == nil {
		t.Fatal("no composite_literal in fixture")
	}
	if got := CompositeLiteralTypeName(literal.ChildByFieldName("type"), source); got != "Worker" {
		t.Errorf("CompositeLiteralTypeName = %q, want %q", got, "Worker")
	}
	if got := CompositeLiteralTypeName(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestEnclosingMethodReceiverFindsNearestMethod(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype S struct{}\n\nfunc (s *S) Handle() {\n\twork()\n}\n\nfunc plain() {\n\tother()\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	lookup := BuildParentLookup(root)

	found := map[string][2]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		name, typ := EnclosingMethodReceiver(node, source, lookup)
		found[string(source[fn.StartByte():fn.EndByte()])] = [2]string{name, typ}
	})

	if got := found["work"]; got != [2]string{"s", "S"} {
		t.Errorf("receiver for work = %v, want [s S]", got)
	}
	if got := found["other"]; got != [2]string{"", ""} {
		t.Errorf("receiver for other = %v, want empty", got)
	}
}

func TestMethodReceiverBindingReadsNameAndType(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype S struct{}\n\nfunc (s *S) Handle() {}\n\nfunc plain() {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	bindings := map[string][2]string{}
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "method_declaration", "function_declaration":
			nameNode := node.ChildByFieldName("name")
			name, typ := MethodReceiverBinding(node, source)
			bindings[string(source[nameNode.StartByte():nameNode.EndByte()])] = [2]string{name, typ}
		}
	})

	if got := bindings["Handle"]; got != [2]string{"s", "S"} {
		t.Errorf("Handle binding = %v, want [s S]", got)
	}
	if got := bindings["plain"]; got != [2]string{"", ""} {
		t.Errorf("plain binding = %v, want empty", got)
	}
	if name, typ := MethodReceiverBinding(nil, source); name != "" || typ != "" {
		t.Errorf("nil node binding = (%q, %q), want empty", name, typ)
	}
}
