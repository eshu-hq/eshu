// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// deepExportTypeScriptSource builds a TypeScript module of classCount exported
// classes (each with methodsPerClass methods) nested under a namespace, so every
// method_definition sits several ancestors below the program root. This is the
// shape that made the JS/TS is-exported and enclosing-context helpers walk
// node.Parent() per declaration in the #3586 profile.
func deepExportTypeScriptSource(classCount, methodsPerClass int) string {
	var b strings.Builder
	b.WriteString("export namespace Outer {\n")
	for i := range classCount {
		fmt.Fprintf(&b, "\texport class Service%d {\n", i)
		for m := range methodsPerClass {
			fmt.Fprintf(&b, "\t\tmethod%d(value: string): boolean { return value.length > %d; }\n", m, m)
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// collectMethodDefinitionNodes returns every method_definition node so the
// regression gate exercises the exact node set the production per-declaration
// ancestor helpers process.
func collectMethodDefinitionNodes(root *tree_sitter.Node) []*tree_sitter.Node {
	var nodes []*tree_sitter.Node
	walkAllTreeNodes(root, func(node *tree_sitter.Node) {
		if node.Kind() == "method_definition" {
			cloned := *node
			nodes = append(nodes, &cloned)
		}
	})
	return nodes
}

// walkAllTreeNodes visits node and every descendant via an iterative stack so
// deep generated bundles cannot blow the goroutine stack.
func walkAllTreeNodes(root *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if root == nil {
		return
	}
	stack := []*tree_sitter.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visit(node)
		count := node.ChildCount()
		for i := range count {
			if child := node.Child(i); child != nil {
				stack = append(stack, child)
			}
		}
	}
}

// isExportedViaCgoParent reproduces the pre-#3586 mechanism: walk node.Parent()
// (a cgo crossing into ts_node_parent per step) up to the program root. It is
// used only as the output-identity oracle and the "the fixture really exercises
// ancestor walks" baseline; production never calls this.
func isExportedViaCgoParent(node *tree_sitter.Node) bool {
	for current := node; current != nil; current = current.Parent() {
		switch current.Kind() {
		case "export_statement":
			return true
		case "program":
			return false
		}
	}
	return false
}

// TestJavaScriptParentLookupEliminatesCgoCrossings is the mechanism regression
// gate for #3586. It exercises the PRODUCTION parent lookup that Parse builds
// (buildJavaScriptParentLookup) and the PRODUCTION is-exported helper
// (javaScriptIsExported), so the gate fails if production code ever drops a
// parent edge or reintroduces a Node.Parent() cgo crossing.
//
// Mechanism. go-tree-sitter's Node.Parent() and Node.Kind() each cross cgo;
// Node.Id() does not. The #3586 optimization replaces the per-step Parent() cgo
// crossing with a pure-Go map lookup keyed by Id(), keeping only the per-node
// Kind() crossing. We measure cgo with runtime.NumCgoCall():
//
//   - The old Parent()-based walk spends one Kind() and one Parent() cgo call
//     per ancestor step.
//   - The production lookup walk spends one Kind() cgo call per ancestor step
//     and zero Parent() calls.
//
// So over identical ancestor walks the production path must make strictly fewer
// cgo calls than the old path, and the difference is exactly the eliminated
// Parent() crossings. The gate asserts: (1) output identity between the two
// mechanisms, (2) the production path makes at most one cgo call per ancestor
// node visited (no Parent() crossings), and (3) the old path makes materially
// more (the regression is real, not a no-op fixture).
func TestJavaScriptParentLookupEliminatesCgoCrossings(t *testing.T) {
	root, _, closeTree := parseTypeScriptRootForTest(t, deepExportTypeScriptSource(40, 6))
	defer closeTree()

	methods := collectMethodDefinitionNodes(root)
	if len(methods) == 0 {
		t.Fatalf("collectMethodDefinitionNodes() = 0 nodes, want > 0")
	}

	// Build the production lookup the same way Parse does, outside both
	// measurement windows so its one-time cost is not attributed to a walk.
	lookup := buildJavaScriptParentLookup(root)

	// Walk every node to the root via the production lookup and confirm each
	// edge matches the real Node.Parent() chain. A dropped or corrupted edge
	// (the realistic regression in buildJavaScriptParentLookup) diverges here.
	ancestorVisits := 0
	for _, node := range methods {
		for current := node; current != nil; current = lookup.parent(current) {
			ancestorVisits++
			want := current.Parent()
			got := lookup.parent(current)
			if (want == nil) != (got == nil) || (want != nil && got != nil && want.Id() != got.Id()) {
				t.Fatalf("production lookup parent edge mismatch at kind %q: lookup=%v Parent()=%v",
					current.Kind(), nodeID(got), nodeID(want))
			}
		}
	}

	// Measure cgo calls made by the PRODUCTION javaScriptIsExported helper.
	gcWarmup() // keep GC-driven cgo (finalizers) out of the window
	beforeProduction := runtime.NumCgoCall()
	for _, node := range methods {
		_ = javaScriptIsExported(node, lookup)
	}
	productionCgo := runtime.NumCgoCall() - beforeProduction

	// Measure cgo calls made by the old Node.Parent() mechanism over the same
	// nodes, and confirm output identity while we are here.
	gcWarmup()
	beforeOld := runtime.NumCgoCall()
	for _, node := range methods {
		oldResult := isExportedViaCgoParent(node)
		if oldResult != javaScriptIsExported(node, lookup) {
			t.Fatalf("is-exported disagreement for node kind %q: old=%v production differs",
				node.Kind(), oldResult)
		}
	}
	oldCgo := runtime.NumCgoCall() - beforeOld
	// The identity loop ran javaScriptIsExported once more per node, so subtract
	// the production-walk cost to isolate the old Parent() walk's cgo.
	oldWalkCgo := oldCgo - productionCgo

	// The production walk makes at most one cgo call (Kind()) per ancestor node
	// it visits and zero Parent() crossings. If a Parent() call were
	// reintroduced, productionCgo would exceed the ancestor-visit budget.
	if productionCgo > int64(ancestorVisits) {
		t.Fatalf("production lookup made %d cgo calls over %d ancestor visits; want <= %d (no Parent() crossings)",
			productionCgo, ancestorVisits, ancestorVisits)
	}

	// The old mechanism must make strictly more cgo calls than production,
	// proving the fixture exercises real ancestor walks and the optimization
	// removes Parent() crossings rather than no-opping.
	if oldWalkCgo <= productionCgo {
		t.Fatalf("old Parent() walk made %d cgo calls, production made %d; want old strictly greater (regression not exercised)",
			oldWalkCgo, productionCgo)
	}

	t.Logf("production lookup walk: %d cgo calls over %d ancestor visits (no Parent() crossings); old Parent() walk: %d cgo calls",
		productionCgo, ancestorVisits, oldWalkCgo)
}

// nodeID renders a node identity for failure messages without crossing cgo for
// nil nodes.
func nodeID(node *tree_sitter.Node) any {
	if node == nil {
		return nil
	}
	return node.Id()
}

// gcWarmup forces a GC cycle so finalizer-driven cgo calls do not land inside a
// NumCgoCall() measurement window and inflate the counted total.
func gcWarmup() {
	runtime.GC()
}
