// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/dataflowemit"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
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
	packageName, localFuncs := javaLocalFunctionIndex(root, source, options.RepositoryID)
	dataflow, findings, effectsByID, sources := javaCollectDataflowFunctions(
		root, source, callInference, options.RepositoryID, packageName, localFuncs,
	)
	if len(dataflow) > 0 {
		payload["dataflow_functions"] = dataflow
	}
	if len(findings) > 0 {
		payload["taint_findings"] = findings
	}
	interprocRows, summaryRows, sourceRows := javaInterprocResults(effectsByID, sources, options.RepositoryID, packageName)
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

// javaCollectDataflowFunctions walks method/constructor declarations once and
// feeds both the per-function dataflow/taint-finding rows and the
// interprocedural effects/sources index from the same lowered cfg.Function
// per node. javaEmitDataflowBuckets and javaInterprocPayloads previously
// walked the tree and lowered every function independently; merging the walk
// also removes the duplicate javaLowerFunction and javaNearestTypeContext
// call per function that the two separate walks used to perform.
func javaCollectDataflowFunctions(
	root *tree_sitter.Node,
	source []byte,
	callInference *javaCallInferenceIndex,
	repositoryID string,
	packageName string,
	localFuncs map[string][]javaLocalFunction,
) (dataflow, findings []map[string]any, effectsByID map[summary.FunctionID]summary.Effects, sources []interproc.Source) {
	limits := cfg.DefaultLimits()
	effectsByID = map[summary.FunctionID]summary.Effects{}
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

		id := javaFunctionID(repositoryID, packageName, classContext, javaFunctionSignatureName(node, source))
		spec := javaEffectsSpec(node, source, fn, callInference, localFuncs)
		effectsByID[id] = valueflow.DeriveEffects(fn, spec)
		sources = append(sources, javaInterprocSources(node, source, id)...)
	})
	dataflowemit.SortFunctionRows(dataflow)
	dataflowemit.SortFindingRows(findings)
	return dataflow, findings, effectsByID, sources
}
