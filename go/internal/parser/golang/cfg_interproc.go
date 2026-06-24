// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/dataflowemit"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goInterprocPayloads derives a value-flow summary for every function in a file,
// renders each as a "dataflow_summaries" row, then composes the summaries into an
// interprocedural port graph and solves it, rendering the cross-function taint
// findings. Resolution is intra-file (a callee is a function defined in the same
// file); cross-file and cross-repo composition is the reducer's job over the
// shared graph — and that composition consumes the per-function summaries, which
// is why they are emitted for every function regardless of whether this file
// produced any finding. Both returned slices are deterministic (summaries sorted
// by function id, findings by the solver).
func goInterprocPayloads(root *tree_sitter.Node, source []byte, repositoryID, importPath string) (findings, summaries, sourceRows []map[string]any) {
	localFuncs := goLocalFunctionIDs(root, source, repositoryID, importPath)
	effectsByID := map[summary.FunctionID]summary.Effects{}
	var sources []interproc.Source

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
		default:
			return
		}
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		id := goFunctionID(repositoryID, importPath, goReceiverContext(node, source), name)
		fn := goLowerFunction(node, source, cfg.DefaultLimits())
		spec := goEffectsSpec(node, source, fn, localFuncs)
		effectsByID[id] = valueflow.DeriveEffects(fn, spec)
		sources = append(sources, goInterprocSources(node, source, id)...)
	})

	// Summaries and the param-level sources are durable persistence facts, so they
	// require the repository identity that keys the FunctionID. The per-file
	// findings below do not, so a bare parser caller still gets them.
	if strings.TrimSpace(repositoryID) != "" && strings.TrimSpace(importPath) != "" {
		summaries = make([]map[string]any, 0, len(effectsByID))
		for id, effects := range effectsByID {
			summaries = append(summaries, dataflowemit.DataflowSummaryRow("go", id, effects))
		}
		dataflowemit.SortSummaryRows(summaries)

		sourceRows = make([]map[string]any, 0, len(sources))
		for _, src := range sources {
			sourceRows = append(sourceRows, dataflowemit.DataflowSourceRow("go", src))
		}
		dataflowemit.SortSourceRows(sourceRows)
	}

	if len(sources) == 0 {
		return nil, summaries, sourceRows
	}
	program := valueflow.BuildProgram(effectsByID, sources, nil)
	result := interproc.SolvePartitioned(program, interproc.DefaultLimits())

	findings = make([]map[string]any, 0, len(result.Findings))
	for _, finding := range result.Findings {
		findings = append(findings, dataflowemit.InterprocFindingRow("go", finding))
	}
	return findings, summaries, sourceRows
}
