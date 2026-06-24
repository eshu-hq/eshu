// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp

import (
	"sort"
	"strconv"
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

// csharpLocalFunction records a callable in the same file keyed for arity- and
// type-disambiguated local-call resolution.
type csharpLocalFunction struct {
	id         summary.FunctionID
	paramTypes []string
}

// csharpInterprocPayloads derives durable cross-function summaries, taint-source
// rows, and interprocedural findings for one C# file. Durable rows require both a
// repository identity and a namespace so the cross-repo fixpoint can reconstruct
// stable function identities.
func csharpInterprocPayloads(
	root *tree_sitter.Node,
	source []byte,
	repositoryID string,
	imports map[string]struct{},
) (findings, summaries, sourceRows []map[string]any) {
	packageName := csharpNamespaceName(root, source)
	localFuncs := csharpLocalFunctionIDs(root, source, repositoryID, packageName)
	effectsByID := map[summary.FunctionID]summary.Effects{}
	var sources []interproc.Source

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if !csharpIsCallableDeclaration(node.Kind()) {
			return
		}
		name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		id := csharpFunctionID(repositoryID, packageName, csharpNearestTypeContext(node, source), csharpFunctionSignatureName(node, source))
		fn := csharpLowerFunction(node, source, cfg.DefaultLimits())
		spec := csharpEffectsSpec(node, source, fn, imports, localFuncs)
		effectsByID[id] = valueflow.DeriveEffects(fn, spec)
		sources = append(sources, csharpInterprocSources(node, source, id, imports)...)
	})

	if strings.TrimSpace(repositoryID) != "" && strings.TrimSpace(packageName) != "" {
		summaries = make([]map[string]any, 0, len(effectsByID))
		for id, effects := range effectsByID {
			summaries = append(summaries, dataflowemit.DataflowSummaryRow("csharp", id, effects))
		}
		dataflowemit.SortSummaryRows(summaries)

		sourceRows = make([]map[string]any, 0, len(sources))
		for _, src := range sources {
			sourceRows = append(sourceRows, dataflowemit.DataflowSourceRow("csharp", src))
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
		findings = append(findings, dataflowemit.InterprocFindingRow("csharp", finding))
	}
	return findings, summaries, sourceRows
}

// csharpIsCallableDeclaration reports the declaration kinds that lower to a
// dataflow function.
func csharpIsCallableDeclaration(kind string) bool {
	switch kind {
	case "method_declaration", "constructor_declaration", "local_function_statement":
		return true
	default:
		return false
	}
}

// csharpFunctionID builds the durable cross-repo identity for a C# callable.
func csharpFunctionID(repositoryID, packageName, classContext, signatureName string) summary.FunctionID {
	return summary.NewFunctionID(repositoryID, packageName, classContext, signatureName)
}

// csharpFunctionSignatureName renders "Name(type,type)" for a callable so
// overloads receive distinct durable identities.
func csharpFunctionSignatureName(node *tree_sitter.Node, source []byte) string {
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
	if name == "" {
		return ""
	}
	return name + "(" + strings.Join(csharpCallableParameterTypes(node, source), ",") + ")"
}

// csharpCallableParameterTypes returns the declared parameter types of a callable
// in declaration order.
func csharpCallableParameterTypes(node *tree_sitter.Node, source []byte) []string {
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	return csharpParameterTypes(params, source)
}

// csharpLocalFunctionIDs indexes same-file callables by (class, name, arity) for
// local-call resolution.
func csharpLocalFunctionIDs(root *tree_sitter.Node, source []byte, repositoryID, packageName string) map[string][]csharpLocalFunction {
	out := map[string][]csharpLocalFunction{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if !csharpIsCallableDeclaration(node.Kind()) {
			return
		}
		name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		classContext := csharpNearestTypeContext(node, source)
		paramTypes := csharpCallableParameterTypes(node, source)
		key := csharpLocalFunctionKey(classContext, name, len(paramTypes))
		out[key] = append(out[key], csharpLocalFunction{
			id:         csharpFunctionID(repositoryID, packageName, classContext, csharpFunctionSignatureName(node, source)),
			paramTypes: paramTypes,
		})
	})
	return out
}

// csharpEffectsSpec collects the structural value-flow slots (params, sources,
// sinks, sanitizers, returns, call args) used to derive a function's effects.
func csharpEffectsSpec(
	funcNode *tree_sitter.Node,
	source []byte,
	fn cfg.Function,
	imports map[string]struct{},
	localFuncs map[string][]csharpLocalFunction,
) valueflow.EffectsSpec {
	index := newCSharpLineIndex(fn)
	env := csharpBuildTypeEnv(funcNode, source)
	spec := valueflow.EffectsSpec{
		Sinks:      map[int]valueflow.SinkSlot{},
		Sanitizers: map[int][]string{},
	}

	funcLine := shared.NodeLine(funcNode)
	for i, name := range csharpDataflowParamNames(funcNode, source) {
		if stmtID, ok := index.defStmt(funcLine, name); ok {
			spec.Params = append(spec.Params, valueflow.ParamSlot{Index: i, Stmt: stmtID, Binding: name})
		}
	}

	facts := csharpTaintFacts(funcNode, source, fn, imports, env)
	for sb, mark := range facts.Sources {
		spec.Sources = append(spec.Sources, valueflow.SourceSlot{Stmt: sb.Stmt, Binding: sb.Binding, Kind: mark.Kind})
	}
	for stmt, mark := range facts.Sinks {
		spec.Sinks[stmt] = valueflow.SinkSlot{Kind: string(mark.Kind)}
	}
	for stmt, mark := range facts.Sanitizers {
		spec.Sanitizers[stmt] = csharpSanitizerKinds(mark)
	}

	spec.Returns = csharpReturnStmts(funcNode, index)
	spec.CallArgs = csharpCallArgSlots(funcNode, source, index, env, localFuncs)
	return spec
}

// csharpSanitizerKinds renders sanitizer-neutralized kinds as strings.
func csharpSanitizerKinds(mark taint.SanitizerMark) []string {
	kinds := make([]string, 0, len(mark.Neutralizes))
	for _, kind := range mark.Neutralizes {
		kinds = append(kinds, string(kind))
	}
	return kinds
}

// csharpReturnStmts returns the sorted CFG statement IDs of return statements.
func csharpReturnStmts(funcNode *tree_sitter.Node, index *csharpLineIndex) []int {
	var stmts []int
	seen := map[int]bool{}
	walkInCSharpFunction(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "return_statement" {
			return
		}
		if stmtID, ok := index.useStmt(shared.NodeLine(node)); ok && !seen[stmtID] {
			seen[stmtID] = true
			stmts = append(stmts, stmtID)
		}
	})
	sort.Ints(stmts)
	return stmts
}

// csharpCallArgSlots records identifier arguments passed to resolvable same-file
// callees so taint can flow across call boundaries.
func csharpCallArgSlots(
	funcNode *tree_sitter.Node,
	source []byte,
	index *csharpLineIndex,
	env csharpTypeEnv,
	localFuncs map[string][]csharpLocalFunction,
) []valueflow.CallArgSlot {
	var slots []valueflow.CallArgSlot
	walkInCSharpFunction(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "invocation_expression" {
			return
		}
		callee, ok := csharpResolveLocalCallee(node, source, env, localFuncs)
		if !ok {
			return
		}
		stmtID, ok := index.useStmt(shared.NodeLine(node))
		if !ok {
			return
		}
		args := node.ChildByFieldName("arguments")
		if args == nil {
			return
		}
		argIndex := 0
		walkDirectNamed(args, func(arg *tree_sitter.Node) {
			if arg.Kind() != "argument" {
				return
			}
			ident := csharpFirstNamedChild(arg)
			current := argIndex
			argIndex++
			if ident == nil || ident.Kind() != "identifier" {
				return
			}
			binding := strings.TrimSpace(shared.NodeText(ident, source))
			if binding == "" {
				return
			}
			slots = append(slots, valueflow.CallArgSlot{Stmt: stmtID, Binding: binding, Callee: callee, Arg: current})
		})
	})
	return slots
}

// csharpResolveLocalCallee resolves an unqualified or `this`-qualified call to a
// same-class callable, using argument types when available to disambiguate
// overloads and falling back to a single unambiguous candidate.
func csharpResolveLocalCallee(
	call *tree_sitter.Node,
	source []byte,
	env csharpTypeEnv,
	localFuncs map[string][]csharpLocalFunction,
) (summary.FunctionID, bool) {
	functionNode := call.ChildByFieldName("function")
	name, ok := csharpUnqualifiedCallName(functionNode, source)
	if !ok || name == "" {
		return "", false
	}
	args := call.ChildByFieldName("arguments")
	argCount := 0
	var argTypes []string
	if args != nil {
		walkDirectNamed(args, func(arg *tree_sitter.Node) {
			if arg.Kind() != "argument" {
				return
			}
			argCount++
			ident := csharpFirstNamedChild(arg)
			if ident != nil && ident.Kind() == "identifier" {
				argTypes = append(argTypes, env.lookup(strings.TrimSpace(shared.NodeText(ident, source))))
			} else {
				argTypes = append(argTypes, "")
			}
		})
	}
	candidates := localFuncs[csharpLocalFunctionKey(csharpNearestTypeContext(call, source), name, argCount)]
	if len(candidates) == 0 {
		return "", false
	}
	if len(argTypes) == argCount && csharpAllNonEmpty(argTypes) {
		for _, candidate := range candidates {
			if csharpSameStringSlice(candidate.paramTypes, argTypes) {
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

// csharpUnqualifiedCallName returns the callee name for an unqualified call or a
// `this.`-qualified call, rejecting other receivers so resolution stays local.
func csharpUnqualifiedCallName(functionNode *tree_sitter.Node, source []byte) (string, bool) {
	if functionNode == nil {
		return "", false
	}
	switch functionNode.Kind() {
	case "identifier":
		return strings.TrimSpace(shared.NodeText(functionNode, source)), true
	case "member_access_expression":
		receiver := functionNode.ChildByFieldName("expression")
		if receiver == nil || strings.TrimSpace(shared.NodeText(receiver, source)) != "this" {
			return "", false
		}
		return strings.TrimSpace(shared.NodeText(functionNode.ChildByFieldName("name"), source)), true
	default:
		return "", false
	}
}

// csharpInterprocSources lists the parameter ports that are taint entry points
// for a callable, used to seed the cross-function fixpoint.
func csharpInterprocSources(funcNode *tree_sitter.Node, source []byte, id summary.FunctionID, imports map[string]struct{}) []interproc.Source {
	sourceKinds := map[string]string{}
	for _, param := range csharpSourceParams(funcNode, source, imports) {
		sourceKinds[param.name] = param.kind
	}
	if len(sourceKinds) == 0 {
		return nil
	}
	var sources []interproc.Source
	for i, name := range csharpDataflowParamNames(funcNode, source) {
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

// csharpLocalFunctionKey keys callables by class context, name, and arity.
func csharpLocalFunctionKey(classContext, name string, arity int) string {
	return classContext + "\x00" + name + "\x00" + strconv.Itoa(arity)
}

// csharpNearestTypeContext returns the simple name of the enclosing type, or
// empty when the callable is top-level.
func csharpNearestTypeContext(node *tree_sitter.Node, source []byte) string {
	name, _, _ := nearestNamedAncestorWithQualifiedKind(
		node, source,
		"class_declaration", "interface_declaration", "struct_declaration", "record_declaration",
	)
	return name
}

// csharpNamespaceName returns the first declared namespace, or empty for the
// global namespace.
func csharpNamespaceName(root *tree_sitter.Node, source []byte) string {
	var namespace string
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if namespace != "" {
			return
		}
		switch node.Kind() {
		case "namespace_declaration", "file_scoped_namespace_declaration":
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				namespace = strings.TrimSpace(shared.NodeText(nameNode, source))
			}
		}
	})
	return namespace
}

// csharpAllNonEmpty reports whether every string is non-empty after trimming.
func csharpAllNonEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

// csharpSameStringSlice reports element-wise string equality.
func csharpSameStringSlice(left, right []string) bool {
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
