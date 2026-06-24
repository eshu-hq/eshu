// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goFunctionID builds a function's summary identity from stable repository
// identity, package import path, receiver, and name.
func goFunctionID(repositoryID, importPath, receiver, name string) summary.FunctionID {
	return summary.NewFunctionID(repositoryID, importPath, receiver, name)
}

// goLocalFunctionIDs maps each top-level function name in a file to its summary
// identity, for intra-file call resolution. Methods are keyed by name only (v1);
// receiver-qualified resolution is a later step.
func goLocalFunctionIDs(root *tree_sitter.Node, source []byte, repositoryID, importPath string) map[string]summary.FunctionID {
	out := map[string]summary.FunctionID{}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := nodeText(node.ChildByFieldName("name"), source)
		if name != "" {
			out[name] = goFunctionID(repositoryID, importPath, "", name)
		}
	})
	return out
}

// goEffectsSpec builds the value-flow EffectsSpec for one Go function from its
// CFG and parsed tree: parameters, the intraprocedural source/sink/sanitizer
// facts, return statements, and call-argument sites that bind a local callee.
func goEffectsSpec(funcNode *tree_sitter.Node, source []byte, fn cfg.Function, localFuncs map[string]summary.FunctionID) valueflow.EffectsSpec {
	index := newGoLineIndex(fn)
	spec := valueflow.EffectsSpec{
		Sinks:      map[int]valueflow.SinkSlot{},
		Sanitizers: map[int][]string{},
	}

	funcLine := nodeLine(funcNode)
	for i, name := range goFunctionParamNames(funcNode, source) {
		if stmtID, ok := index.defStmt(funcLine, name); ok {
			spec.Params = append(spec.Params, valueflow.ParamSlot{Index: i, Stmt: stmtID, Binding: name})
		}
	}

	facts := goTaintFacts(funcNode, source, fn)
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

	spec.Returns = goReturnStmts(funcNode, index)
	spec.CallArgs = goCallArgSlots(funcNode, source, index, localFuncs, goEarliestDefLines(fn))
	return spec
}

// goEarliestDefLines maps each binding defined in the function to the earliest
// source line that defines it. A bare call is treated as shadowed by a local
// value only when a definition of that name exists at or before the call's line
// (parameters are defined at the entry line, so they shadow the whole body).
// This is a line-ordering approximation of Go's lexical scoping: a local declared
// after a call does not shadow it, so the earlier call still resolves to the
// package-level function.
func goEarliestDefLines(fn cfg.Function) map[string]int {
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

// goReturnStmts returns the CFG statement IDs of value-returning statements.
func goReturnStmts(funcNode *tree_sitter.Node, index *goLineIndex) []int {
	var stmts []int
	seen := map[int]bool{}
	walkScopeBindings(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "return_statement" {
			return
		}
		if stmtID, ok := index.useStmt(nodeLine(node)); ok && !seen[stmtID] {
			seen[stmtID] = true
			stmts = append(stmts, stmtID)
		}
	})
	sort.Ints(stmts)
	return stmts
}

// goCallArgSlots returns the call-argument sites that pass a bare identifier into
// a locally-defined callee, so cross-function value flow can be composed.
//
// Only bare-identifier calls (foo(...)) are resolved against the local function
// table; a method or qualified call (db.Query(...), pkg.Fn(...)) is never a local
// function, so it is skipped — otherwise a method whose name matched a local
// function would produce a false cross-function edge. The call site is located by
// source line, so two calls on one line are a known intra-file limit (a safe
// false negative, never a false edge). A call whose name is shadowed by a
// parameter or a local binding defined at or before the call (a function value)
// is not resolved to the package-level function, which would also be a false
// edge; a local declared after the call does not shadow it.
func goCallArgSlots(funcNode *tree_sitter.Node, source []byte, index *goLineIndex, localFuncs map[string]summary.FunctionID, defLines map[string]int) []valueflow.CallArgSlot {
	var slots []valueflow.CallArgSlot
	walkScopeBindings(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fnNode := node.ChildByFieldName("function")
		if fnNode == nil || fnNode.Kind() != "identifier" {
			return
		}
		name := nodeText(fnNode, source)
		if defLine, ok := defLines[name]; ok && defLine <= nodeLine(node) {
			return // shadowed by a binding defined at or before this call
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
			if binding == "" || binding == blankIdentifier {
				continue
			}
			slots = append(slots, valueflow.CallArgSlot{
				Stmt: stmtID, Binding: binding, Callee: callee, Arg: argIndex,
			})
		}
		cursor.Close()
	})
	return slots
}

// goInterprocSources returns interprocedural taint sources for a function's
// source parameters (for example an *http.Request parameter), at their parameter
// ports.
func goInterprocSources(funcNode *tree_sitter.Node, source []byte, id summary.FunctionID) []interproc.Source {
	sourceParams := goSourceParams(funcNode, source)
	if len(sourceParams) == 0 {
		return nil
	}
	var sources []interproc.Source
	for i, name := range goFunctionParamNames(funcNode, source) {
		kind, ok := sourceParams[name]
		if !ok {
			continue
		}
		sources = append(sources, interproc.Source{
			Port: interproc.Port{Func: interproc.FunctionID(id), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: i}},
			Kind: kind,
		})
	}
	return sources
}
