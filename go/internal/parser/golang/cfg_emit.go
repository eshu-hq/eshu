// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/dataflowemit"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goEmitDataflowBuckets lowers every top-level Go function and method once and
// renders two deterministic, bounded payload buckets: per-function control-flow
// and reaching-definition facts ("dataflow_functions"), and intraprocedural
// taint findings ("taint_findings"). Function literals are skipped here;
// closures are modeled by a later pass. The rows are rendered and sorted by the
// shared internal/parser/dataflowemit renderer so every language emits one schema.
func goEmitDataflowBuckets(root *tree_sitter.Node, source []byte) (dataflow, findings []map[string]any) {
	limits := cfg.DefaultLimits()
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
		default:
			return
		}
		nameNode := node.ChildByFieldName("name")
		name := strings.TrimSpace(nodeText(nameNode, source))
		if name == "" {
			return
		}
		line := nodeLine(nameNode)
		classContext := goReceiverContext(node, source)

		fn := goLowerFunction(node, source, limits)
		dataflow = append(dataflow, dataflowemit.DataflowFunctionRow("go", name, line, classContext, fn))

		facts := goTaintFacts(node, source, fn)
		result := taint.Analyze(fn, facts, taint.DefaultLimits())
		for _, finding := range result.Findings {
			findings = append(findings, dataflowemit.TaintFindingRow("go", name, line, classContext, finding))
		}
	})
	dataflowemit.SortFunctionRows(dataflow)
	dataflowemit.SortFindingRows(findings)
	return dataflow, findings
}
