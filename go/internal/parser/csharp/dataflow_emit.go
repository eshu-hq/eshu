// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/dataflowemit"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// emitCSharpValueFlowBuckets populates the opt-in value-flow payload buckets for
// a parsed C# file. It is a no-op unless options.EmitDataflow is set, keeping the
// default snapshot byte-identical to a non-dataflow parse.
func emitCSharpValueFlowBuckets(
	payload map[string]any,
	root *tree_sitter.Node,
	source []byte,
	options shared.Options,
) {
	if !options.EmitDataflow {
		return
	}
	imports := csharpImportSet(root, source)
	payload["dataflow_catalog_versions"] = []map[string]any{
		dataflowemit.CatalogVersionRow("csharp", "taint", csharpTaintCatalogVersion()),
	}
	dataflow, findings := csharpEmitDataflowBuckets(root, source, imports)
	if len(dataflow) > 0 {
		payload["dataflow_functions"] = dataflow
	}
	if len(findings) > 0 {
		payload["taint_findings"] = findings
	}
	interprocRows, summaryRows, sourceRows := csharpInterprocPayloads(root, source, options.RepositoryID, imports)
	if len(interprocRows) > 0 {
		payload["interproc_findings"] = interprocRows
	}
	if len(summaryRows) > 0 {
		payload["dataflow_summaries"] = summaryRows
	}
	if len(sourceRows) > 0 {
		payload["dataflow_sources"] = sourceRows
	}
}

// csharpEmitDataflowBuckets lowers each callable to a CFG row and runs the
// intraprocedural taint analysis, returning the deterministic
// "dataflow_functions" and "taint_findings" rows.
func csharpEmitDataflowBuckets(
	root *tree_sitter.Node,
	source []byte,
	imports map[string]struct{},
) (dataflow, findings []map[string]any) {
	limits := cfg.DefaultLimits()
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if !csharpIsCallableDeclaration(node.Kind()) {
			return
		}
		nameNode := node.ChildByFieldName("name")
		name := strings.TrimSpace(shared.NodeText(nameNode, source))
		if name == "" {
			return
		}
		line := shared.NodeLine(nameNode)
		classContext := csharpNearestTypeContext(node, source)
		fn := csharpLowerFunction(node, source, limits)
		dataflow = append(dataflow, dataflowemit.DataflowFunctionRow("csharp", name, line, classContext, fn))

		env := csharpBuildTypeEnv(node, source)
		facts := csharpTaintFacts(node, source, fn, imports, env)
		result := taint.Analyze(fn, facts, taint.DefaultLimits())
		for _, finding := range result.Findings {
			findings = append(findings, dataflowemit.TaintFindingRow("csharp", name, line, classContext, finding))
		}
	})
	dataflowemit.SortFunctionRows(dataflow)
	dataflowemit.SortFindingRows(findings)
	return dataflow, findings
}
