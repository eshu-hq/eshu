// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestGoVariableTypeIndexForNodeRespectsBindingOrder pins the invariant the
// per-binding delta cache relies on: ForNode must apply only the bindings that
// precede the query node (startByte <= target) and, when the same variable is
// rebound, the later binding must win. A regression that applied every scope
// binding regardless of position, or in the wrong order, would resolve `x` to
// the wrong struct type at the earlier call site. This exercises the real
// VariableTypeIndex, not a re-implementation, so it fails if the cached-delta
// merge in ForNode stops honoring startByte order.
func TestGoVariableTypeIndexForNodeRespectsBindingOrder(t *testing.T) {
	t.Parallel()

	source := []byte(`package p

type Foo struct{}
type Bar struct{}

func run() {
	x := Foo{}
	useFirst(x)
	x = Bar{}
	useSecond(x)
}
`)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()

	lookup := BuildParentLookup(root)
	structTypes := map[string]struct{}{"foo": {}, "bar": {}}
	idx := BuildVariableTypeIndex(root, source, structTypes, map[string]string{}, lookup)

	// Collect the two marker call expressions by callee name.
	calls := map[string]*tree_sitter.Node{}
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
			calls[name] = node
		}
	})
	if calls["useFirst"] == nil || calls["useSecond"] == nil {
		t.Fatalf("fixture parse did not yield both marker calls: %v", calls)
	}

	first := idx.ForNode(root, calls["useFirst"])
	if got := first["x"]; got != "foo" {
		t.Errorf("ForNode at useFirst: x = %q, want %q (only the earlier binding should apply)", got, "foo")
	}

	second := idx.ForNode(root, calls["useSecond"])
	if got := second["x"]; got != "bar" {
		t.Errorf("ForNode at useSecond: x = %q, want %q (later rebinding must win)", got, "bar")
	}
}
