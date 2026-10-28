// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// inferenceFixtureSource rebinds x from A to B so the two marker calls
// observe different applicable binding sets.
const inferenceFixtureSource = `package p

type A struct{}
type B struct{}

func run() {
	x := A{}
	useFirst(x)
	x = B{}
	useSecond(x)
}
`

func parseInferenceFixture(t *testing.T) (*tree_sitter.Tree, []byte, map[string]int) {
	t.Helper()
	source := []byte(inferenceFixtureSource)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	t.Cleanup(func() { tree.Close() })
	root := tree.RootNode()

	lines := map[string]int{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		if fn == nil {
			return
		}
		name := string(source[fn.StartByte():fn.EndByte()])
		if name == "useFirst" || name == "useSecond" {
			lines[name] = shared.NodeLine(node)
		}
	})
	if lines["useFirst"] == 0 || lines["useSecond"] == 0 {
		t.Fatal("fixture parse did not yield both marker calls")
	}
	return tree, source, lines
}

func TestInferredReceiverTypeRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	tree, source, lines := parseInferenceFixture(t)
	bindings := LocalReceiverBindings(tree.RootNode(), source, nil, BuildParentLookup(tree.RootNode()))

	for name, tc := range map[string]struct {
		receiver string
		line     int
	}{
		"blank receiver": {"  ", lines["useFirst"]},
		"unknown name":   {"missing", lines["useFirst"]},
		"zero line":      {"x", 0},
		"negative line":  {"x", -3},
	} {
		if got := InferredReceiverType(tc.receiver, tc.line, bindings); got != "" {
			t.Errorf("%s: InferredReceiverType = %q, want %q", name, got, "")
		}
		if got := ConcreteInferredReceiverType(tc.receiver, tc.line, bindings); got != "" {
			t.Errorf("%s: ConcreteInferredReceiverType = %q, want %q", name, got, "")
		}
	}
}

func TestInferredReceiverTypePrefersLaterBinding(t *testing.T) {
	t.Parallel()

	tree, source, lines := parseInferenceFixture(t)
	bindings := LocalReceiverBindings(tree.RootNode(), source, nil, BuildParentLookup(tree.RootNode()))

	if got := InferredReceiverType("x", lines["useFirst"], bindings); got != "A" {
		t.Errorf("at useFirst: InferredReceiverType(x) = %q, want %q", got, "A")
	}
	if got := InferredReceiverType("x", lines["useSecond"], bindings); got != "B" {
		t.Errorf("at useSecond: InferredReceiverType(x) = %q, want %q", got, "B")
	}
}

func TestConcreteInferredReceiverTypeRequiresSingleConcreteType(t *testing.T) {
	t.Parallel()

	tree, source, lines := parseInferenceFixture(t)
	bindings := LocalReceiverBindings(tree.RootNode(), source, nil, BuildParentLookup(tree.RootNode()))

	if got := ConcreteInferredReceiverType("x", lines["useFirst"], bindings); got != "A" {
		t.Errorf("at useFirst: ConcreteInferredReceiverType(x) = %q, want %q", got, "A")
	}
	if got := ConcreteInferredReceiverType("x", lines["useSecond"], bindings); got != "" {
		t.Errorf("at useSecond: ConcreteInferredReceiverType(x) = %q, want empty (two concrete types observed)", got)
	}
}

func TestConcreteInferredReceiverTypeVetoedByLaterUnresolvedRebinding(t *testing.T) {
	t.Parallel()

	source := []byte(`package p

type A struct{}

func run(opaque any) {
	y := A{}
	useBefore(y)
	y = opaque
	useAfter(y)
}
`)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	bindings := LocalReceiverBindings(root, source, nil, BuildParentLookup(root))

	lines := map[string]int{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		if fn == nil {
			return
		}
		name := string(source[fn.StartByte():fn.EndByte()])
		if name == "useBefore" || name == "useAfter" {
			lines[name] = shared.NodeLine(node)
		}
	})
	if lines["useBefore"] == 0 || lines["useAfter"] == 0 {
		t.Fatal("fixture parse did not yield both marker calls")
	}

	if got := ConcreteInferredReceiverType("y", lines["useBefore"], bindings); got != "A" {
		t.Errorf("before reassignment: ConcreteInferredReceiverType(y) = %q, want %q", got, "A")
	}
	if got := ConcreteInferredReceiverType("y", lines["useAfter"], bindings); got != "" {
		t.Errorf("after unresolved reassignment: ConcreteInferredReceiverType(y) = %q, want empty", got)
	}
	// The plain inference still reports the last applicable binding.
	if got := InferredReceiverType("y", lines["useAfter"], bindings); got != "A" {
		t.Errorf("after reassignment: InferredReceiverType(y) = %q, want %q", got, "A")
	}
}

func TestInferredReceiverTypePrefersNarrowestScope(t *testing.T) {
	t.Parallel()

	source := []byte(`package p

type Outer struct{}
type Inner struct{}

func run(flag bool) {
	w := Outer{}
	if flag {
		w := Inner{}
		useInner(w)
	}
	useOuter(w)
}
`)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	bindings := LocalReceiverBindings(root, source, nil, BuildParentLookup(root))

	lines := map[string]int{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		if fn == nil {
			return
		}
		name := string(source[fn.StartByte():fn.EndByte()])
		if name == "useInner" || name == "useOuter" {
			lines[name] = shared.NodeLine(node)
		}
	})
	if lines["useInner"] == 0 || lines["useOuter"] == 0 {
		t.Fatal("fixture parse did not yield both marker calls")
	}

	if got := InferredReceiverType("w", lines["useInner"], bindings); got != "Inner" {
		t.Errorf("inside block: InferredReceiverType(w) = %q, want %q", got, "Inner")
	}
	if got := InferredReceiverType("w", lines["useOuter"], bindings); got != "Outer" {
		t.Errorf("outside block: InferredReceiverType(w) = %q, want %q", got, "Outer")
	}
}
