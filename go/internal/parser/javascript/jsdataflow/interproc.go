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

// InterprocFindings derives a value-flow summary for every top-level function in
// a TS/JS file, composes them into an interprocedural port graph, and solves it,
// returning the cross-function taint findings. Resolution is intra-file; cross-
// file and cross-repo composition is the reducer's job over the shared graph. The
// findings are deterministic (the solver sorts them).
func InterprocFindings(root *tree_sitter.Node, source []byte, repositoryID, importPath string) []interproc.Finding {
	localFuncs := LocalFunctionIDs(root, source, repositoryID, importPath)
	summaries := map[summary.FunctionID]summary.Effects{}
	var sources []interproc.Source

	var walk func(*tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "function_declaration" {
			if name := nodeText(node.ChildByFieldName("name"), source); name != "" {
				id := FunctionID(repositoryID, importPath, name)
				fn := LowerFunction(node, source, cfg.DefaultLimits())
				spec := EffectsSpec(node, source, fn, localFuncs)
				summaries[id] = valueflow.DeriveEffects(fn, spec)
				sources = append(sources, interprocSources(node, source, id)...)
			}
			// Do not descend into the body: a nested function is lexically private
			// and is not a file-level interprocedural entry (closures are a later
			// pass). This mirrors LocalFunctionIDs so the two stay consistent.
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

	if len(sources) == 0 {
		return nil
	}
	program := valueflow.BuildProgram(summaries, sources, nil)
	return interproc.SolvePartitioned(program, interproc.DefaultLimits()).Findings
}
