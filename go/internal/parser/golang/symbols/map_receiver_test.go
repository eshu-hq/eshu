// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// mapFixtureSource declares a map parameter, a map var_spec, a plain var,
// and a local interface so the merged collect walk has one of everything.
const mapFixtureSource = `package p

type Worker struct{}
type Store interface{ Get() string }

func run(m map[string]Worker, n int) {
	var table map[string]Worker
	plain := 1
	for k, v := range m {
		use(v, k)
	}
	use(table, plain, n)
}
`

func parseMapFixture(t *testing.T) (*tree_sitter.Tree, []byte) {
	t.Helper()
	source := []byte(mapFixtureSource)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	t.Cleanup(func() { tree.Close() })
	return tree, source
}

func TestCollectMapValueTypesAndInterfaceNames(t *testing.T) {
	t.Parallel()

	tree, source := parseMapFixture(t)
	root := tree.RootNode()
	lookup := BuildParentLookup(root)

	bindings, ifaces := goCollectLocalMapValueTypesAndInterfaceNames(root, source, lookup)

	if _, ok := ifaces["store"]; !ok {
		t.Errorf("interface names = %v, want store", ifaces)
	}
	byVar := make(map[string]string)
	for _, binding := range bindings {
		byVar[binding.variable] = binding.typeName
	}
	for variable, want := range map[string]string{"m": "Worker", "table": "Worker"} {
		if got := byVar[variable]; got != want {
			t.Errorf("map binding for %q = %q, want %q (all bindings: %v)", variable, got, want, byVar)
		}
	}
	if _, ok := byVar["n"]; ok {
		t.Errorf("non-map parameter n has a map-value binding, want none (all: %v)", byVar)
	}
}

func TestMapValueTypeForNamePicksLatestInScope(t *testing.T) {
	t.Parallel()

	bindings := []goLocalMapValueTypeBinding{
		{variable: "m", typeName: "Worker", line: 2, scopeStart: 1, scopeEnd: 20},
		{variable: "m", typeName: "Client", line: 9, scopeStart: 1, scopeEnd: 20},
		{variable: "m", typeName: "Late", line: 30, scopeStart: 1, scopeEnd: 40},
		{variable: "other", typeName: "Worker", line: 2, scopeStart: 1, scopeEnd: 20},
	}

	if got := goMapValueTypeForName("m", 10, bindings); got != "Client" {
		t.Errorf("at line 10: goMapValueTypeForName(m) = %q, want %q", got, "Client")
	}
	if got := goMapValueTypeForName("m", 3, bindings); got != "Worker" {
		t.Errorf("at line 3: goMapValueTypeForName(m) = %q, want %q", got, "Worker")
	}
	if got := goMapValueTypeForName("missing", 10, bindings); got != "" {
		t.Errorf("unknown variable = %q, want empty", got)
	}
	for name, tc := range map[string]struct {
		variable string
		line     int
	}{"blank variable": {"  ", 10}, "zero line": {"m", 0}} {
		if got := goMapValueTypeForName(tc.variable, tc.line, bindings); got != "" {
			t.Errorf("%s: = %q, want empty", name, got)
		}
	}
}

func TestRangeValueAndSourceNames(t *testing.T) {
	t.Parallel()

	source := []byte(`package p

func run(m map[string]int, items []string, jobs chan int) {
	for k, v := range m {
		use(k, v)
	}
	for x := range items {
		use(x)
	}
	for range jobs {
		work()
	}
	for i := 0; i < 10; i++ {
		use(i)
	}
}
`)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	type pair struct{ value, right string }
	// Collect the range clauses in source order.
	clauses := []*tree_sitter.Node{}
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() == "range_clause" {
			clauses = append(clauses, node)
		}
	})
	if len(clauses) != 3 {
		t.Fatalf("found %d range clauses, want 3", len(clauses))
	}
	want := []pair{{"v", "m"}, {"", "items"}, {"", "jobs"}}
	for i, clause := range clauses {
		value, right := goRangeValueAndSourceNames(clause, source)
		if value != want[i].value || right != want[i].right {
			t.Errorf("clause %d: = (%q, %q), want (%q, %q)", i, value, right, want[i].value, want[i].right)
		}
	}

	// A blank value name and a non-range statement yield no value.
	if value, right := goRangeValueAndSourceNames(clauses[2], source); value != "" || right != "jobs" {
		t.Errorf("blank-value clause = (%q, %q), want (%q, %q)", value, right, "", "jobs")
	}
	var classic *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if classic == nil && node.Kind() == "for_statement" && FirstNamedDescendant(node, "range_clause") == nil {
			classic = node
		}
	})
	if classic == nil {
		t.Fatal("no classic for statement in fixture")
	}
	if value, right := goRangeValueAndSourceNames(classic, source); value != "" || right != "" {
		t.Errorf("classic for = (%q, %q), want empty", value, right)
	}
}

func TestMapValueTypeNameFromNodeRequiresMapType(t *testing.T) {
	t.Parallel()

	tree, source := parseMapFixture(t)
	root := tree.RootNode()

	var mapType, paramType *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "map_type":
			if mapType == nil {
				mapType = node
			}
		case "parameter_declaration":
			if paramType == nil {
				if typeNode := node.ChildByFieldName("type"); typeNode != nil && typeNode.Kind() == "qualified_type" {
					paramType = typeNode
				}
			}
		}
	})
	if mapType == nil {
		t.Fatal("no map_type in fixture")
	}
	if got := goMapValueTypeNameFromNode(mapType, source); got != "Worker" {
		t.Errorf("map value type = %q, want %q", got, "Worker")
	}
	if got := goMapValueTypeNameFromNode(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}

	var ident *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	if got := goMapValueTypeNameFromNode(ident, source); got != "" {
		t.Errorf("non-map node = %q, want empty", got)
	}
	_ = paramType
}

func TestMapValueTypeNameFromExpressionOnlyUnwrapsCompositeLiterals(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Worker struct{}\n\nvar m = map[string]Worker{}\n\nvar n = other\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	nodes := map[string]*tree_sitter.Node{}
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() == "var_spec" {
			name := node.ChildByFieldName("name")
			if name != nil {
				nodes[string(source[name.StartByte():name.EndByte()])] = node.ChildByFieldName("value")
			}
		}
	})
	if nodes["m"] == nil || nodes["n"] == nil {
		t.Fatal("fixture did not yield both var values")
	}
	if got := goMapValueTypeNameFromExpression(nodes["m"], source); got != "Worker" {
		t.Errorf("composite literal value type = %q, want %q", got, "Worker")
	}
	if got := goMapValueTypeNameFromExpression(nodes["n"], source); got != "" {
		t.Errorf("identifier value type = %q, want empty", got)
	}
	if got := goMapValueTypeNameFromExpression(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestRangeClauseBindingThroughForStatement(t *testing.T) {
	t.Parallel()

	tree, source := parseMapFixture(t)
	root := tree.RootNode()
	lookup := BuildParentLookup(root)
	mapValueTypes, _ := goCollectLocalMapValueTypesAndInterfaceNames(root, source, lookup)

	var forNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if forNode == nil && node.Kind() == "for_statement" {
			forNode = node
		}
	})
	if forNode == nil {
		t.Fatal("no for_statement in fixture")
	}

	bindings := goLocalReceiverBindingsFromRangeClause(forNode, source, mapValueTypes, nil, lookup)
	if len(bindings) != 1 {
		t.Fatalf("range bindings = %#v, want exactly one", bindings)
	}
	if bindings[0].Variable != "v" || bindings[0].typeName != "Worker" || !bindings[0].concrete {
		t.Errorf("range binding = %#v, want v/Worker/concrete", bindings[0])
	}
}

func TestNewMapValueTypeBindingWithoutScopeIsZeroValue(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nvar m map[string]int\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	lookup := BuildParentLookup(root)

	var spec *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if spec == nil && node.Kind() == "var_spec" {
			spec = node
		}
	})
	if spec == nil {
		t.Fatal("no var_spec in fixture")
	}
	name := IdentifierNodes(spec.ChildByFieldName("name"), source)
	if len(name) == 0 {
		t.Fatal("no name node in var_spec")
	}
	binding := goNewMapValueTypeBinding(spec, name[0], "Int", source, lookup)
	if binding.variable != "" {
		t.Errorf("package-level map binding = %#v, want zero value", binding)
	}
}
