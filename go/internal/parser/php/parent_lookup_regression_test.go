// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

func TestPHPParentLookupEliminatesScopeContextParentCgoCrossings(t *testing.T) {
	root, source, closeTree := parsePHPRootForTest(t, deepPHPContextSource(48))
	defer closeTree()

	variables := collectPHPVariableNodes(root)
	if len(variables) == 0 {
		t.Fatalf("collectPHPVariableNodes() = 0 nodes, want > 0")
	}

	lookup := buildPHPParentLookup(root)
	ancestorVisits := 0
	for _, node := range variables {
		for current := node; current != nil; current = lookup.parent(current) {
			ancestorVisits++
			want := current.Parent()
			got := lookup.parent(current)
			if (want == nil) != (got == nil) || (want != nil && got != nil && want.Id() != got.Id()) {
				t.Fatalf("parent edge mismatch at kind %q: lookup=%v Parent()=%v",
					current.Kind(), phpNodeID(got), phpNodeID(want))
			}
		}
	}

	for _, node := range variables {
		if got, want := phpScopeKeyForNode(node, source, lookup), phpScopeKeyForNodeViaCgoParent(node, source); got != want {
			t.Fatalf("phpScopeKeyForNode() = %q, want %q", got, want)
		}
		got := phpResolveContext(node, source, lookup)
		want := phpResolveContextViaCgoParent(node, source)
		if got != want {
			t.Fatalf("phpResolveContext() = %+v, want %+v", got, want)
		}
	}

	gcWarmup()
	beforeProduction := runtime.NumCgoCall()
	for _, node := range variables {
		_ = phpScopeKeyForNode(node, source, lookup)
		_ = phpResolveContext(node, source, lookup)
	}
	productionCgo := runtime.NumCgoCall() - beforeProduction

	gcWarmup()
	beforeOld := runtime.NumCgoCall()
	for _, node := range variables {
		_ = phpScopeKeyForNodeViaCgoParent(node, source)
		_ = phpResolveContextViaCgoParent(node, source)
	}
	oldCgo := runtime.NumCgoCall() - beforeOld

	if productionCgo >= oldCgo {
		t.Fatalf("production lookup made %d cgo calls, old Parent() walk made %d over %d ancestor visits; want production lower",
			productionCgo, oldCgo, ancestorVisits)
	}

	t.Logf("production lookup scope/context walk: %d cgo calls; old Parent() walk: %d cgo calls over %d ancestor visits",
		productionCgo, oldCgo, ancestorVisits)
}

func TestPHPParentLookupEliminatesAssignmentParentCgoCrossings(t *testing.T) {
	root, source, closeTree := parsePHPRootForTest(t, assignmentHeavyPHPSource(36))
	defer closeTree()

	variables := collectPHPVariableNodes(root)
	if len(variables) == 0 {
		t.Fatalf("collectPHPVariableNodes() = 0 nodes, want > 0")
	}

	lookup := buildPHPParentLookup(root)
	for _, node := range variables {
		got := phpEnclosingAssignment(node, lookup)
		want := phpEnclosingAssignmentViaCgoParent(node)
		if (want == nil) != (got == nil) || (want != nil && got != nil && want.Id() != got.Id()) {
			t.Fatalf("phpEnclosingAssignment() mismatch for %q: lookup=%v Parent()=%v",
				strings.TrimSpace(shared.NodeText(node, source)), phpNodeID(got), phpNodeID(want))
		}
	}

	gcWarmup()
	beforeProduction := runtime.NumCgoCall()
	for _, node := range variables {
		_ = phpEnclosingAssignment(node, lookup)
	}
	productionCgo := runtime.NumCgoCall() - beforeProduction

	gcWarmup()
	beforeOld := runtime.NumCgoCall()
	for _, node := range variables {
		_ = phpEnclosingAssignmentViaCgoParent(node)
	}
	oldCgo := runtime.NumCgoCall() - beforeOld

	if productionCgo >= oldCgo {
		t.Fatalf("production assignment lookup made %d cgo calls, old Parent() walk made %d; want production lower",
			productionCgo, oldCgo)
	}

	t.Logf("production assignment lookup: %d cgo calls; old Parent() walk: %d cgo calls",
		productionCgo, oldCgo)
}

func parsePHPRootForTest(t *testing.T, source string) (*tree_sitter.Node, []byte, func()) {
	t.Helper()

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_php.LanguagePHP())); err != nil {
		t.Fatalf("SetLanguage(PHP) error = %v", err)
	}
	tree := parser.Parse([]byte(source), nil)
	if tree == nil {
		t.Fatalf("Parse(PHP) returned nil tree")
	}
	return tree.RootNode(), []byte(source), tree.Close
}

func deepPHPContextSource(depth int) string {
	var builder strings.Builder
	builder.WriteString("<?php\n")
	builder.WriteString("class Demo {\n")
	builder.WriteString("  public function run($input) {\n")
	for i := range depth {
		builder.WriteString(strings.Repeat(" ", i+4))
		builder.WriteString("if ($input) {\n")
	}
	for i := range depth {
		builder.WriteString(strings.Repeat(" ", depth-i+4))
		builder.WriteString("$value")
		builder.WriteString(strings.Repeat("->next()", i%5))
		builder.WriteString(";\n")
	}
	for i := depth - 1; i >= 0; i-- {
		builder.WriteString(strings.Repeat(" ", i+4))
		builder.WriteString("}\n")
	}
	builder.WriteString("  }\n")
	builder.WriteString("}\n")
	return builder.String()
}

func assignmentHeavyPHPSource(depth int) string {
	var builder strings.Builder
	builder.WriteString("<?php\n")
	builder.WriteString("class Demo {\n")
	builder.WriteString("  public function run($input) {\n")
	for i := range depth {
		builder.WriteString(strings.Repeat(" ", i+4))
		builder.WriteString("if ($input) {\n")
		builder.WriteString(strings.Repeat(" ", i+6))
		builder.WriteString("$value")
		builder.WriteString(strings.Repeat("->next()", i%4))
		builder.WriteString(" = new Service")
		builder.WriteString(";")
		builder.WriteString("\n")
	}
	for i := depth - 1; i >= 0; i-- {
		builder.WriteString(strings.Repeat(" ", i+4))
		builder.WriteString("}\n")
	}
	builder.WriteString("  }\n")
	builder.WriteString("}\n")
	return builder.String()
}

func collectPHPVariableNodes(root *tree_sitter.Node) []*tree_sitter.Node {
	var nodes []*tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "variable_name" {
			nodes = append(nodes, node)
		}
	})
	return nodes
}

func phpScopeKeyForNodeViaCgoParent(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_definition", "method_declaration":
			functionName := strings.TrimSpace(shared.NodeText(current.ChildByFieldName("name"), source))
			typeName := phpNearestTypeContextViaCgoParent(current, source)
			return phpFunctionScopeKey(typeName, functionName)
		}
	}
	return ""
}

func phpEnclosingAssignmentViaCgoParent(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "assignment_expression":
			return current
		case "expression_statement", "compound_statement", "method_declaration", "function_definition":
			return nil
		}
	}
	return nil
}

func phpResolveContextViaCgoParent(node *tree_sitter.Node, source []byte) phpCallContext {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_definition", "method_declaration":
			nameNode := current.ChildByFieldName("name")
			return phpCallContext{
				name: strings.TrimSpace(shared.NodeText(nameNode, source)),
				kind: phpFunctionContextKind(current),
				line: shared.NodeLine(phpNameNode(current)),
			}
		case "class_declaration", "interface_declaration", "trait_declaration":
			nameNode := current.ChildByFieldName("name")
			return phpCallContext{
				name: strings.TrimSpace(shared.NodeText(nameNode, source)),
				kind: current.Kind(),
				line: shared.NodeLine(phpNameNode(current)),
			}
		}
	}
	return phpCallContext{}
}

func phpNearestTypeContextViaCgoParent(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "trait_declaration":
			return strings.TrimSpace(shared.NodeText(current.ChildByFieldName("name"), source))
		case "anonymous_class":
			return phpAnonymousClassName(shared.NodeLine(current))
		}
	}
	return ""
}

func phpNodeID(node *tree_sitter.Node) any {
	if node == nil {
		return nil
	}
	return node.Id()
}

func gcWarmup() {
	runtime.GC()
}
