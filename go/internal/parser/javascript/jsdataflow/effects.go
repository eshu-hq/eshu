// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jsdataflow

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// FunctionID builds a function's summary identity from stable repository
// identity, package import path, and name.
func FunctionID(repositoryID, importPath, name string) summary.FunctionID {
	return summary.NewFunctionID(repositoryID, importPath, "", name)
}

// LocalFunctionIDs maps each top-level function declaration name in a file to its
// summary identity, for intra-file call resolution. It records a function
// declaration but does not descend into its body: a function nested inside
// another function is lexically private to it and must not become visible to
// unrelated top-level callers, which would invent a false cross-function edge.
func LocalFunctionIDs(root *tree_sitter.Node, source []byte, repositoryID, importPath string) map[string]summary.FunctionID {
	out := map[string]summary.FunctionID{}
	var walk func(*tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "function_declaration" {
			if name := nodeText(node.ChildByFieldName("name"), source); name != "" {
				out[name] = FunctionID(repositoryID, importPath, name)
			}
			// Do not descend: nested declarations are not file-visible.
			return
		}
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			walk(&child)
		}
	}
	walk(root)
	return out
}

// EffectsSpec builds the value-flow EffectsSpec for one TS/JS function from its
// CFG and parsed tree: parameters, intraprocedural source/sink/sanitizer facts,
// return statements, and call-argument sites that bind a local callee.
func EffectsSpec(funcNode *tree_sitter.Node, source []byte, fn cfg.Function, localFuncs map[string]summary.FunctionID) valueflow.EffectsSpec {
	index := newLineIndex(fn)
	spec := valueflow.EffectsSpec{
		Sinks:      map[int]valueflow.SinkSlot{},
		Sanitizers: map[int][]string{},
	}

	funcLine := nodeLine(funcNode)
	for _, param := range paramBindings(funcNode, source) {
		if stmtID, ok := index.defStmt(funcLine, param.name); ok {
			spec.Params = append(spec.Params, valueflow.ParamSlot{Index: param.index, Stmt: stmtID, Binding: param.name})
		}
	}

	facts := TaintFacts(funcNode, source, fn)
	for sb, mark := range facts.Sources {
		spec.Sources = append(spec.Sources, valueflow.SourceSlot{Stmt: sb.Stmt, Binding: sb.Binding, Kind: mark.Kind})
	}
	for stmt, mark := range facts.Sinks {
		spec.Sinks[stmt] = valueflow.SinkSlot{Kind: string(mark.Kind)}
	}
	for stmt, mark := range facts.Sanitizers {
		kinds := make([]string, 0, len(mark.Neutralizes))
		for _, k := range mark.Neutralizes {
			kinds = append(kinds, string(k))
		}
		spec.Sanitizers[stmt] = kinds
	}

	spec.Returns = returnStmts(funcNode, index)
	spec.CallArgs = callArgSlots(funcNode, source, index, localFuncs, earliestDefLines(fn))
	return spec
}

// earliestDefLines maps each binding defined in the function to the earliest line
// that defines it, so a call shadowed by a local binding declared at or before
// the call is not resolved to a package-level function.
func earliestDefLines(fn cfg.Function) map[string]int {
	earliest := map[string]int{}
	for _, block := range fn.Blocks {
		for _, stmt := range block.Stmts {
			for _, def := range stmt.Defs {
				if line, ok := earliest[def]; !ok || stmt.Line < line {
					earliest[def] = stmt.Line
				}
			}
		}
	}
	return earliest
}

// returnStmts returns the CFG statement IDs of value-returning statements.
func returnStmts(funcNode *tree_sitter.Node, index *lineIndex) []int {
	var stmts []int
	seen := map[int]bool{}
	walkInFunction(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "return_statement" {
			return
		}
		if stmtID, ok := index.useStmt(nodeLine(node)); ok && !seen[stmtID] {
			seen[stmtID] = true
			stmts = append(stmts, stmtID)
		}
	})
	return stmts
}

// callArgSlots returns the call-argument sites that pass a bare identifier into a
// locally-defined callee. Only bare-identifier calls resolve (a member call like
// db.query is never a local function), and a call whose name is shadowed by a
// binding defined at or before the call is skipped, so neither produces a false
// cross-function edge.
func callArgSlots(funcNode *tree_sitter.Node, source []byte, index *lineIndex, localFuncs map[string]summary.FunctionID, defLines map[string]int) []valueflow.CallArgSlot {
	var slots []valueflow.CallArgSlot
	walkInFunction(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fnNode := node.ChildByFieldName("function")
		if fnNode == nil || fnNode.Kind() != "identifier" {
			return
		}
		name := nodeText(fnNode, source)
		if defLine, ok := defLines[name]; ok && defLine <= nodeLine(node) {
			return
		}
		callee, ok := localFuncs[name]
		if !ok {
			return
		}
		stmtID, ok := index.useStmt(nodeLine(node))
		if !ok {
			return
		}
		args := node.ChildByFieldName("arguments")
		if args == nil {
			return
		}
		cursor := args.Walk()
		for argIndex, arg := range args.NamedChildren(cursor) {
			arg := arg
			if arg.Kind() != "identifier" {
				continue
			}
			binding := nodeText(&arg, source)
			if binding == "" {
				continue
			}
			slots = append(slots, valueflow.CallArgSlot{Stmt: stmtID, Binding: binding, Callee: callee, Arg: argIndex})
		}
		cursor.Close()
	})
	return slots
}

// interprocSources returns interprocedural taint sources for a function's typed
// framework request parameters, at their parameter ports.
func interprocSources(funcNode *tree_sitter.Node, source []byte, id summary.FunctionID) []interproc.Source {
	var sources []interproc.Source
	sourceKinds := map[string]string{}
	for _, param := range sourceParams(funcNode, source) {
		sourceKinds[param.Name] = param.Kind
	}
	for _, param := range paramBindings(funcNode, source) {
		kind, ok := sourceKinds[param.name]
		if !ok {
			continue
		}
		sources = append(sources, interproc.Source{
			Port: interproc.Port{Func: interproc.FunctionID(id), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: param.index}},
			Kind: kind,
		})
	}
	return sources
}
