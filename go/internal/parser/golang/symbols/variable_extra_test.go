// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestLocalParameterTypesKeepsOnlyStructParams(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Worker struct{}\n\nfunc run(w Worker, n int) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var funcNode *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if funcNode == nil && node.Kind() == "function_declaration" {
			funcNode = node
		}
	})
	got := goLocalParameterTypes(funcNode, source, map[string]struct{}{"worker": {}})
	if !reflect.DeepEqual(got, map[string]string{"w": "worker"}) {
		t.Errorf("goLocalParameterTypes() = %#v, want {w: worker}", got)
	}

	recorded := make(map[string]string)
	goRecordLocalParameterTypes(funcNode, source, map[string]struct{}{"worker": {}}, recorded)
	if !reflect.DeepEqual(recorded, got) {
		t.Errorf("goRecordLocalParameterTypes() = %#v, want %#v", recorded, got)
	}
}

func TestConcreteTypeFromConstructorCallMatchesKnownConstructors(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc run() {\n\ta := NewWorker()\n\tb := unknown()\n\tc := pkg.New()\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	structs := map[string]struct{}{"worker": {}}
	returns := map[string]string{"NewWorker": "Worker"}

	// The grammar wraps a single-element right side in an expression_list,
	// so callers unwrap first; exercise both the direct call shape and the
	// real caller path through UnwrapSingleExpression.
	values := map[string]string{}
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() != "short_var_declaration" {
			return
		}
		left := node.ChildByFieldName("left")
		right := UnwrapSingleExpression(node.ChildByFieldName("right"))
		values[string(source[left.StartByte():left.EndByte()])] = goConcreteTypeFromConstructorCall(right, source, structs, returns)
	})
	for variable, want := range map[string]string{"a": "worker", "b": "", "c": ""} {
		if got := values[variable]; got != want {
			t.Errorf("constructor type of %s = %q, want %q", variable, got, want)
		}
	}
	var selectorCall *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if selectorCall == nil && node.Kind() == "call_expression" {
			if fn := node.ChildByFieldName("function"); fn != nil && fn.Kind() == "selector_expression" {
				selectorCall = node
			}
		}
	})
	if got := goConcreteTypeFromConstructorCall(selectorCall, source, structs, returns); got != "" {
		t.Errorf("selector call = %q, want empty (identifier calls only)", got)
	}
	if got := goConcreteTypeFromConstructorCall(nil, source, structs, returns); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestConcreteTypeFromConstructorCallFallsBackToLowercaseName(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc run() {\n\ta := NEWWORKER()\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var call *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if call == nil && node.Kind() == "call_expression" {
			call = node
		}
	})
	structs := map[string]struct{}{"worker": {}}
	returns := map[string]string{"newworker": "Worker"}
	if got := goConcreteTypeFromConstructorCall(call, source, structs, returns); got != "worker" {
		t.Errorf("lowercase fallback = %q, want %q", got, "worker")
	}
}

func TestImportedVariableTypeIndexForCall(t *testing.T) {
	t.Parallel()

	source := []byte(`package p

import "example.com/pkg"

var packageClient pkg.Client

func run(local pkg.Handle) {
	shadow := 1
	_ = shadow
	pkg.Do(packageClient)
	pkg.Do(local)
}
`)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	lookup := BuildParentLookup(root)
	aliases := ImportAliasIndex(root, source)
	idx := BuildImportedVariableTypeIndex(root, source, aliases, lookup)

	calls := []*tree_sitter.Node{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "call_expression" {
			calls = append(calls, node)
		}
	})
	if len(calls) != 2 {
		t.Fatalf("found %d pkg.Do calls, want 2", len(calls))
	}

	// Both calls see the package-level client; the parameter type resolves
	// through the scope bindings.
	for _, call := range calls {
		got := idx.ForCall(call)
		if got["packageclient"] != "example.com/pkg.client" {
			t.Errorf("ForCall missing package var: %#v", got)
		}
	}
	if got := idx.ForCall(calls[1]); got["local"] != "example.com/pkg.handle" {
		t.Errorf("second call map = %#v, want scoped param local resolved", got)
	}

	// The returned map is a copy: mutating it must not poison the cache.
	probe := idx.ForCall(calls[0])
	probe["packageclient"] = "mutated"
	if again := idx.ForCall(calls[0]); again["packageclient"] != "example.com/pkg.client" {
		t.Errorf("cache poisoned by caller mutation: %#v", again)
	}

	// A nil index answers empty instead of panicking.
	var nilIdx *ImportedVariableTypeIndex
	if got := nilIdx.ForCall(calls[0]); len(got) != 0 {
		t.Errorf("nil index ForCall = %#v, want empty", got)
	}
}

func TestImportedDirectMethodCallKeyResolvesReceiverType(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc run() {\n\tclient.Close()\n\tplain()\n\tunknown.Method()\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	variableTypes := map[string]string{"client": "example.com/pkg.client"}

	keys := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		keys[string(source[fn.StartByte():fn.EndByte()])] = ImportedDirectMethodCallKey(node, source, nil, variableTypes, nil)
	})

	if got := keys["client.Close"]; got != "example.com/pkg.client.close" {
		t.Errorf("method key = %q, want %q", got, "example.com/pkg.client.close")
	}
	if got := keys["plain"]; got != "" {
		t.Errorf("plain call key = %q, want empty", got)
	}
	if got := keys["unknown.Method"]; got != "" {
		t.Errorf("unknown receiver key = %q, want empty", got)
	}
}

func TestImportedFmtStringerCallKeysNeedsQualifiedType(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nimport \"fmt\"\n\nfunc run(w pkg.Widget, n int) {\n\tfmt.Sprintf(\"%v %v\", w, n)\n\tplain(w)\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	aliases := ImportAliasIndex(root, source)
	variableTypes := map[string]string{"w": "example.com/pkg.widget"}

	found := map[string][]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		found[string(source[fn.StartByte():fn.EndByte()])] = ImportedFmtStringerCallKeys(node, source, aliases, variableTypes, nil)
	})

	if got := found["fmt.Sprintf"]; !reflect.DeepEqual(got, []string{"example.com/pkg.widget.string"}) {
		t.Errorf("stringer keys = %#v, want [example.com/pkg.widget.string]", got)
	}
	if got := found["plain"]; len(got) != 0 {
		t.Errorf("non-fmt call keys = %#v, want empty", got)
	}
}

func TestKnownLocalPackageVariableTypesSkipsFunctionBodies(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Worker struct{}\n\nfunc NewWorker() Worker { return Worker{} }\n\nvar shared = Worker{}\n\nvar built = NewWorker()\n\nvar count = 1\n\nfunc run() {\n\tlocal := Worker{}\n\t_ = local\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	structs := map[string]struct{}{"worker": {}}
	returns := map[string]string{"NewWorker": "Worker"}

	got := goKnownLocalPackageVariableTypes(root, source, structs, returns, BuildParentLookup(root))
	// A composite literal resolves through the value shape. A var_spec
	// constructor call does not: the grammar wraps the value in an
	// expression_list, which neither the composite nor the constructor
	// probe unwraps on the var_spec path.
	if got["shared"] != "worker" {
		t.Errorf("package var shared = %q, want %q (all: %#v)", got["shared"], "worker", got)
	}
	if _, ok := got["built"]; ok {
		t.Errorf("package var built resolved to %q, want absent (var_spec constructor edge)", got["built"])
	}
	for _, variable := range []string{"count", "local"} {
		if _, ok := got[variable]; ok {
			t.Errorf("variable %s present in package map, want absent", variable)
		}
	}
}

func TestWalkScopeBindingsSkipsNestedDefinitions(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc outer() {\n\tx := 1\n\tinner := func() {\n\t\ty := 2\n\t\t_ = y\n\t}\n\t_ = x\n\t_ = inner\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var funcNode *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if funcNode == nil && node.Kind() == "function_declaration" {
			funcNode = node
		}
	})
	visited := []string{}
	WalkScopeBindings(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() == "short_var_declaration" {
			left := node.ChildByFieldName("left")
			visited = append(visited, string(source[left.StartByte():left.EndByte()]))
		}
	})
	if !reflect.DeepEqual(visited, []string{"x", "inner"}) {
		t.Errorf("scope walk declarations = %#v, want [x inner] (closure body skipped)", visited)
	}
	WalkScopeBindings(nil, func(*tree_sitter.Node) {
		t.Error("nil scope visit called, want no visit")
	})
}

func TestFirstNamedDescendantFindsFirstMatch(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc run() {\n\tf(a, b)\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var call *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if call == nil && node.Kind() == "call_expression" {
			call = node
		}
	})
	// Pre-order visits the function identifier before the argument list,
	// so the single-kind queries pin what "first" means for each kind.
	found := FirstNamedDescendant(call, "argument_list")
	if found == nil || found.Kind() != "argument_list" {
		t.Errorf("argument_list query = %#v, want the argument_list", found)
	}
	firstIdent := FirstNamedDescendant(call, "identifier")
	if firstIdent == nil || string(source[firstIdent.StartByte():firstIdent.EndByte()]) != "f" {
		t.Errorf("identifier query did not return f first: %#v", firstIdent)
	}
	if got := FirstNamedDescendant(call, "no_such_kind"); got != nil {
		t.Errorf("missing kind = %#v, want nil", got)
	}
}

func TestReceiverTypeFromAnyTypeNodeFallsBackToNormalizedText(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nimport \"example.com/pkg\"\n\nfunc use(c pkg.Client, n []int) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	aliases := ImportAliasIndex(root, source)

	types := []string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "parameter_declaration" {
			types = append(types, goReceiverTypeFromAnyTypeNode(node.ChildByFieldName("type"), source, aliases))
		}
	})
	if !reflect.DeepEqual(types, []string{"example.com/pkg.client", "int"}) {
		t.Errorf("receiver types = %#v, want [example.com/pkg.client int]", types)
	}
}

func TestImportedTypeFromSelectorOperandClassifiesShapes(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nimport \"example.com/pkg\"\n\nfunc use(c pkg.Client) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	aliases := ImportAliasIndex(root, source)

	var qualified *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if qualified == nil && node.Kind() == "qualified_type" {
			qualified = node
		}
	})
	if qualified == nil {
		t.Fatal("no qualified_type in fixture")
	}
	if got := goImportedTypeFromSelectorOperand(qualified, source, aliases); got != "example.com/pkg.client" {
		t.Errorf("qualified type = %q, want %q", got, "example.com/pkg.client")
	}

	var ident *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	if got := goImportedTypeFromSelectorOperand(ident, source, aliases); got != "" {
		t.Errorf("identifier operand = %q, want empty", got)
	}
	if got := goImportedTypeFromSelectorOperand(nil, source, aliases); got != "" {
		t.Errorf("nil operand = %q, want empty", got)
	}
}

func TestLocalInterfaceImportedMethodReturnsMapsImportedResults(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nimport \"example.com/pkg\"\n\ntype Store interface {\n\tLoad() pkg.Client\n\tName() string\n}\n\ntype Plain interface {\n\tM()\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	aliases := ImportAliasIndex(tree.RootNode(), source)

	got := LocalInterfaceImportedMethodReturns(tree.RootNode(), source, aliases)
	if got["store.load"] != "example.com/pkg.client" {
		t.Errorf("store.load = %q, want %q (all: %#v)", got["store.load"], "example.com/pkg.client", got)
	}
	if _, ok := got["store.name"]; ok {
		t.Errorf("store.name present with builtin result, want absent (all: %#v)", got)
	}
}
