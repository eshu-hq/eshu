// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/dataflowemit"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type javaLocalFunction struct {
	id         summary.FunctionID
	paramTypes []string
}

// javaInterprocResults finishes the interprocedural analysis from the
// effects/sources that javaCollectDataflowFunctions gathered in its single
// method/constructor walk: it builds the summary and source rows (when
// repositoryID and packageName are both known) and solves the interproc
// program for cross-function findings. Splitting this out of the walk
// itself is what lets the dataflow-function walk and the interproc walk
// share one tree traversal instead of each performing its own.
func javaInterprocResults(
	effectsByID map[summary.FunctionID]summary.Effects,
	sources []interproc.Source,
	repositoryID string,
	packageName string,
) (findings, summaries, sourceRows []map[string]any) {
	if strings.TrimSpace(repositoryID) != "" && strings.TrimSpace(packageName) != "" {
		summaries = make([]map[string]any, 0, len(effectsByID))
		for id, effects := range effectsByID {
			summaries = append(summaries, dataflowemit.DataflowSummaryRow("java", id, effects))
		}
		dataflowemit.SortSummaryRows(summaries)

		sourceRows = make([]map[string]any, 0, len(sources))
		for _, src := range sources {
			sourceRows = append(sourceRows, dataflowemit.DataflowSourceRow("java", src))
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
		findings = append(findings, dataflowemit.InterprocFindingRow("java", finding))
	}
	return findings, summaries, sourceRows
}

func javaFunctionID(repositoryID, packageName, classContext, signatureName string) summary.FunctionID {
	return summary.NewFunctionID(repositoryID, packageName, classContext, signatureName)
}

func javaFunctionSignatureName(node *tree_sitter.Node, source []byte) string {
	name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
	if name == "" {
		return ""
	}
	return name + "(" + strings.Join(javaParameterTypes(node, source), ",") + ")"
}

// javaLocalFunctionIndex resolves the file's package name and its local
// method/constructor FunctionID index in one tree walk. The FunctionID for
// each declaration depends on packageName, which can only be known once the
// (single, file-scoped) package_declaration has been seen, so the walk
// collects the declaration nodes and assigns FunctionIDs in a second,
// non-walking pass once packageName is final. This preserves the exact
// javaPackageName and javaLocalFunctionIDs behavior it replaces, just
// merged into a single full-tree traversal.
func javaLocalFunctionIndex(
	root *tree_sitter.Node,
	source []byte,
	repositoryID string,
) (string, map[string][]javaLocalFunction) {
	var packageName string
	var declNodes []*tree_sitter.Node
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "package_declaration":
			if packageName != "" {
				return
			}
			walkDirectNamed(node, func(child *tree_sitter.Node) {
				if packageName == "" {
					packageName = strings.TrimSpace(nodeText(child, source))
				}
			})
			if packageName == "" {
				raw := strings.TrimSpace(nodeText(node, source))
				raw = strings.TrimPrefix(raw, "package")
				raw = strings.TrimSuffix(raw, ";")
				packageName = strings.TrimSpace(raw)
			}
		case "method_declaration", "constructor_declaration":
			declNodes = append(declNodes, cloneNode(node))
		}
	})

	out := map[string][]javaLocalFunction{}
	for _, node := range declNodes {
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			continue
		}
		classContext := javaNearestTypeContext(node, source)
		paramTypes := javaParameterTypes(node, source)
		key := javaLocalFunctionKey(classContext, name, len(paramTypes))
		out[key] = append(out[key], javaLocalFunction{
			id:         javaFunctionID(repositoryID, packageName, classContext, javaFunctionSignatureName(node, source)),
			paramTypes: paramTypes,
		})
	}
	return packageName, out
}

func javaEffectsSpec(
	funcNode *tree_sitter.Node,
	source []byte,
	fn cfg.Function,
	callInference *javaCallInferenceIndex,
	localFuncs map[string][]javaLocalFunction,
) valueflow.EffectsSpec {
	index := newJavaLineIndex(fn)
	spec := valueflow.EffectsSpec{
		Sinks:      map[int]valueflow.SinkSlot{},
		Sanitizers: map[int][]string{},
	}

	funcLine := nodeLine(funcNode)
	for i, name := range javaDataflowParamNames(funcNode, source) {
		if stmtID, ok := index.defStmt(funcLine, name); ok {
			spec.Params = append(spec.Params, valueflow.ParamSlot{Index: i, Stmt: stmtID, Binding: name})
		}
	}

	facts := javaTaintFacts(funcNode, source, fn, callInference)
	for sb, mark := range facts.Sources {
		spec.Sources = append(spec.Sources, valueflow.SourceSlot{Stmt: sb.Stmt, Binding: sb.Binding, Kind: mark.Kind})
	}
	for stmt, mark := range facts.Sinks {
		spec.Sinks[stmt] = valueflow.SinkSlot{Kind: string(mark.Kind)}
	}
	for stmt, mark := range facts.Sanitizers {
		spec.Sanitizers[stmt] = javaSanitizerKinds(mark)
	}

	spec.Returns = javaReturnStmts(funcNode, index)
	spec.CallArgs = javaCallArgSlots(funcNode, source, index, callInference, localFuncs)
	return spec
}

func javaSanitizerKinds(mark taint.SanitizerMark) []string {
	kinds := make([]string, 0, len(mark.Neutralizes))
	for _, kind := range mark.Neutralizes {
		kinds = append(kinds, string(kind))
	}
	return kinds
}

func javaReturnStmts(funcNode *tree_sitter.Node, index *javaLineIndex) []int {
	var stmts []int
	seen := map[int]bool{}
	walkInJavaFunction(funcNode, func(node *tree_sitter.Node) {
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

func javaCallArgSlots(
	funcNode *tree_sitter.Node,
	source []byte,
	index *javaLineIndex,
	callInference *javaCallInferenceIndex,
	localFuncs map[string][]javaLocalFunction,
) []valueflow.CallArgSlot {
	var slots []valueflow.CallArgSlot
	walkInJavaFunction(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "method_invocation" {
			return
		}
		callee, ok := javaResolveLocalCallee(node, source, callInference, localFuncs)
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
		defer cursor.Close()
		for argIndex, arg := range args.NamedChildren(cursor) {
			arg := arg
			if arg.Kind() != "identifier" {
				continue
			}
			binding := strings.TrimSpace(nodeText(&arg, source))
			if binding == "" {
				continue
			}
			slots = append(slots, valueflow.CallArgSlot{Stmt: stmtID, Binding: binding, Callee: callee, Arg: argIndex})
		}
	})
	return slots
}

func javaResolveLocalCallee(
	call *tree_sitter.Node,
	source []byte,
	callInference *javaCallInferenceIndex,
	localFuncs map[string][]javaLocalFunction,
) (summary.FunctionID, bool) {
	name := strings.TrimSpace(nodeText(call.ChildByFieldName("name"), source))
	if name == "" {
		return "", false
	}
	if objectNode := call.ChildByFieldName("object"); objectNode != nil {
		if receiver := strings.TrimSpace(nodeText(objectNode, source)); receiver != "this" {
			return "", false
		}
	}
	args := call.ChildByFieldName("arguments")
	argCount := 0
	if args != nil {
		walkDirectNamed(args, func(*tree_sitter.Node) { argCount++ })
	}
	candidates := localFuncs[javaLocalFunctionKey(javaNearestTypeContext(call, source), name, argCount)]
	if len(candidates) == 0 {
		return "", false
	}
	argTypes := javaCallArgumentTypes(call, source, callInference)
	if len(argTypes) == argCount && allNonEmptyStrings(argTypes) {
		for _, candidate := range candidates {
			if sameStringSlice(candidate.paramTypes, argTypes) {
				return candidate.id, true
			}
		}
		return "", false
	}
	if len(candidates) == 1 {
		return candidates[0].id, true
	}
	return "", false
}

func javaInterprocSources(funcNode *tree_sitter.Node, source []byte, id summary.FunctionID) []interproc.Source {
	imports := javaImportSet(funcNode, source)
	sourceKinds := map[string]string{}
	for _, param := range javaSourceParams(funcNode, source, imports) {
		sourceKinds[param.name] = param.kind
	}
	if len(sourceKinds) == 0 {
		return nil
	}
	var sources []interproc.Source
	for i, name := range javaDataflowParamNames(funcNode, source) {
		kind, ok := sourceKinds[name]
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

func javaLocalFunctionKey(classContext, name string, arity int) string {
	return classContext + "\x00" + name + "\x00" + strconv.Itoa(arity)
}

func allNonEmptyStrings(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func sameStringSlice(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
