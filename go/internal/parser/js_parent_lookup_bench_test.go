// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// BenchmarkParsePathTypeScriptExportHeavy parses a synthetic TypeScript file
// with many exported classes, methods, and interfaces nested several levels
// deep under export_statement and namespace wrappers. The JS/TS dead-code and
// export-surface helpers walk node.Parent() per declaration to recover ancestor
// context (is-exported, enclosing class, CommonJS plugin object). Before the
// per-parse javaScriptParentLookup landed, each Parent() crossed cgo into
// ts_node_parent and the pattern scaled as O(n_declarations * depth) cgo
// crossings per file, making runtime.cgocall ~48% of all parse CPU on a
// full-corpus profile (see #3586). This benchmark is the focused regression
// gate that proves the JS/TS parse path stays bounded.
func BenchmarkParsePathTypeScriptExportHeavy(b *testing.B) {
	repoRoot := b.TempDir()
	filePath := filepath.Join(repoRoot, "heavy.ts")
	writeBenchFile(b, filePath, generateExportHeavyTypeScriptSource(120, 12))

	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for b.Loop() {
		if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
			b.Fatalf("ParsePath() error = %v, want nil", err)
		}
	}
}

// generateExportHeavyTypeScriptSource produces TypeScript source with many
// exported classes that each declare a constructor and methods, plus exported
// interfaces and a namespace wrapper that adds ancestor depth. The shape
// exercises every helper that walked node.Parent() per declaration:
// javaScriptIsExported, javaScriptConstructorClass, javaScriptEnclosingClassName,
// and the TypeScript export-surface root-kind helpers.
func generateExportHeavyTypeScriptSource(classCount, methodsPerClass int) string {
	var b strings.Builder
	for i := range classCount {
		fmt.Fprintf(&b, "export interface Contract%d {\n", i)
		for m := range methodsPerClass {
			fmt.Fprintf(&b, "\tmethod%d(value: string): boolean;\n", m)
		}
		b.WriteString("}\n\n")
	}
	b.WriteString("export namespace Registry {\n")
	for i := range classCount {
		fmt.Fprintf(&b, "\texport class Service%d implements Contract%d {\n", i, i)
		fmt.Fprintf(&b, "\t\tconstructor(private readonly dep%d: string) {}\n", i)
		for m := range methodsPerClass {
			fmt.Fprintf(&b, "\t\tmethod%d(value: string): boolean {\n", m)
			fmt.Fprintf(&b, "\t\t\treturn value !== this.dep%d && value.length > %d;\n", i, m)
			b.WriteString("\t\t}\n")
		}
		b.WriteString("\t}\n\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// deepExportTypeScriptSource builds a TypeScript module of classCount exported
// classes (each with methodsPerClass methods) nested under a namespace, so
// every method_definition sits several ancestors below the program root. This
// is the shape that made the JS/TS is-exported and enclosing-context helpers
// walk node.Parent() per declaration in the #3586 profile.
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

// parseTypeScriptTree parses source with the package's real tree-sitter
// TypeScript grammar so the parent-lookup benchmarks and counts operate on a
// genuine syntax tree.
func parseTypeScriptTree(tb testing.TB, source string) *tree_sitter.Node {
	tb.Helper()
	parser, err := NewRuntime().Parser("typescript")
	if err != nil {
		tb.Fatalf("Parser(typescript) error = %v, want nil", err)
	}
	tb.Cleanup(parser.Close)
	tree := parser.Parse([]byte(source), nil)
	if tree == nil {
		tb.Fatalf("Parse returned nil tree")
	}
	tb.Cleanup(tree.Close)
	return tree.RootNode()
}

// collectMethodDefinitionNodes returns every method_definition node so the
// parent-lookup measurements exercise the exact node set the per-declaration
// ancestor helpers process.
func collectMethodDefinitionNodes(root *tree_sitter.Node) []*tree_sitter.Node {
	var nodes []*tree_sitter.Node
	walkAllNodes(root, func(node *tree_sitter.Node) {
		if node.Kind() == "method_definition" {
			cloned := *node
			nodes = append(nodes, &cloned)
		}
	})
	return nodes
}

// walkAllNodes visits node and every descendant via an iterative stack.
func walkAllNodes(root *tree_sitter.Node, visit func(*tree_sitter.Node)) {
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

// isExportedViaCgoParent reproduces the pre-#3586 mechanism used as the
// baseline for BenchmarkJavaScriptIsExportedCgoParent: walk node.Parent() (a
// cgo crossing into ts_node_parent per step) up to the program root. This is
// benchmark scaffolding only; production code was replaced by
// javaScriptIsExported in the javascript package.
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

// benchParentLookup is a local benchmark scaffold that mirrors the shape of
// the production javaScriptParentLookup in go/internal/parser/javascript. It
// is used only by BenchmarkJavaScriptIsExportedParentLookup to measure the
// speedup story independently of the parser integration path. The mechanism
// regression gate (TestJavaScriptParentLookupEliminatesCgoCrossings) lives in
// go/internal/parser/javascript and exercises the production helpers directly.
type benchParentLookup struct {
	parents map[uintptr]*tree_sitter.Node
}

func buildBenchParentLookup(root *tree_sitter.Node) *benchParentLookup {
	lookup := &benchParentLookup{parents: make(map[uintptr]*tree_sitter.Node)}
	walkAllNodes(root, func(node *tree_sitter.Node) {
		count := node.ChildCount()
		for i := range count {
			if child := node.Child(i); child != nil {
				lookup.parents[child.Id()] = node
			}
		}
	})
	return lookup
}

func (l *benchParentLookup) parent(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	return l.parents[node.Id()]
}

// isExportedViaLookup walks ancestors through the Go-side parent map, matching
// the pattern of the production javaScriptIsExported helper. Used only by
// BenchmarkJavaScriptIsExportedParentLookup to measure per-call overhead.
func isExportedViaLookup(node *tree_sitter.Node, lookup *benchParentLookup) bool {
	for current := node; current != nil; current = lookup.parent(current) {
		switch current.Kind() {
		case "export_statement":
			return true
		case "program":
			return false
		}
	}
	return false
}

// ancestorWalksPerNode models the real per-declaration ancestor-walk density of
// the JS/TS dead-code and semantic helpers. A single declaration node feeds
// is-exported, enclosing-class, enclosing-function, CommonJS-export, and several
// framework helpers, each of which walks ancestors. The #3586 profile showed
// many such walks per node; counting several per node keeps the isolation
// benchmark honest about how often the helpers re-enter ts_node_parent for one
// declaration during one Parse.
const ancestorWalksPerNode = 8

// BenchmarkJavaScriptIsExportedCgoParent measures the old mechanism for one
// Parse: every declaration node feeds several ancestor-walking helpers, and
// each Parent() step is a cgo crossing into ts_node_parent. Cost scales as
// O(n_methods * walks_per_node * depth) cgo crossings.
func BenchmarkJavaScriptIsExportedCgoParent(b *testing.B) {
	root := parseTypeScriptTree(b, deepExportTypeScriptSource(200, 8))
	methods := collectMethodDefinitionNodes(root)
	b.ReportMetric(float64(len(methods)), "method_nodes")
	b.ResetTimer()
	for b.Loop() {
		for _, node := range methods {
			for range ancestorWalksPerNode {
				_ = isExportedViaCgoParent(node)
			}
		}
	}
}

// BenchmarkJavaScriptIsExportedParentLookup measures the new mechanism for one
// Parse: the parent map is built once per tree (as production does at the top of
// Parse), then every ancestor walk is pure Go map lookups with zero cgo. The
// one-time build cost is amortized across all per-declaration helper walks.
func BenchmarkJavaScriptIsExportedParentLookup(b *testing.B) {
	root := parseTypeScriptTree(b, deepExportTypeScriptSource(200, 8))
	methods := collectMethodDefinitionNodes(root)
	b.ReportMetric(float64(len(methods)), "method_nodes")
	b.ResetTimer()
	for b.Loop() {
		lookup := buildBenchParentLookup(root)
		for _, node := range methods {
			for range ancestorWalksPerNode {
				_ = isExportedViaLookup(node, lookup)
			}
		}
	}
}
