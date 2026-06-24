// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/dataflowemit"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func emitJavaValueFlowBuckets(
	payload map[string]any,
	root *tree_sitter.Node,
	source []byte,
	options shared.Options,
	callInference *javaCallInferenceIndex,
) {
	if !options.EmitDataflow {
		return
	}
	payload["dataflow_catalog_versions"] = []map[string]any{
		dataflowemit.CatalogVersionRow("java", "taint", javaTaintCatalogVersion()),
	}
	dataflow, findings := javaEmitDataflowBuckets(root, source, callInference)
	if len(dataflow) > 0 {
		payload["dataflow_functions"] = dataflow
	}
	if len(findings) > 0 {
		payload["taint_findings"] = findings
	}
	interprocRows, summaryRows, sourceRows := javaInterprocPayloads(root, source, options.RepositoryID, callInference)
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

func javaEmitDataflowBuckets(
	root *tree_sitter.Node,
	source []byte,
	callInference *javaCallInferenceIndex,
) (dataflow, findings []map[string]any) {
	limits := cfg.DefaultLimits()
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_declaration" && node.Kind() != "constructor_declaration" {
			return
		}
		nameNode := node.ChildByFieldName("name")
		name := strings.TrimSpace(nodeText(nameNode, source))
		if name == "" {
			return
		}
		line := nodeLine(nameNode)
		classContext := javaNearestTypeContext(node, source)
		fn := javaLowerFunction(node, source, limits)
		dataflow = append(dataflow, dataflowemit.DataflowFunctionRow("java", name, line, classContext, fn))

		facts := javaTaintFacts(node, source, fn, callInference)
		result := taint.Analyze(fn, facts, taint.DefaultLimits())
		for _, finding := range result.Findings {
			findings = append(findings, dataflowemit.TaintFindingRow("java", name, line, classContext, finding))
		}
	})
	dataflowemit.SortFunctionRows(dataflow)
	dataflowemit.SortFindingRows(findings)
	return dataflow, findings
}
