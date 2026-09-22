// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// LocalReceiverBinding records one lexically-scoped observation of a local
// variable's concrete or interface-typed receiver type, used to resolve the
// receiver type of a method call by the nearest enclosing binding in scope
// at the call site. Variable is exported because callers outside this
// package (the AWS SDK receiver-binding path) test it directly to detect the
// zero value returned when a binding could not be constructed.
type LocalReceiverBinding struct {
	// Variable is the bound local name, or "" for the zero value.
	Variable   string
	typeName   string
	concrete   bool
	line       int
	scopeStart int
	scopeEnd   int
}

// LocalNameBinding records one lexically-scoped local name declaration
// (a parameter or a local variable), used to test whether a package-level
// name is shadowed at a given call site.
type LocalNameBinding struct {
	variable   string
	line       int
	scopeStart int
	scopeEnd   int
}

// CollectConstructorReturnType records the constructor return type for one
// function_declaration node into returns, if any. It is the single-node
// visitor used by goCollectFileLevelIndexes (the per-file merged walk in
// Parse; see #4839) that replaced the standalone goConstructorReturnTypes
// full-tree walk.
func CollectConstructorReturnType(node *tree_sitter.Node, source []byte, returns map[string]string) {
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
	if name == "" {
		return
	}
	typeName := TypeNameFromNode(node.ChildByFieldName("result"), source)
	if typeName == "" {
		return
	}
	returns[name] = typeName
}

// LocalNameBindingsFromParameters scopes parameter names to the function
// body, matching Go's lexical visibility for parameters.
func LocalNameBindingsFromParameters(node *tree_sitter.Node, source []byte) []LocalNameBinding {
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	parameters := node.ChildByFieldName("parameters")
	if parameters == nil {
		return nil
	}
	bindings := make([]LocalNameBinding, 0)
	WalkDirectNamed(parameters, func(child *tree_sitter.Node) {
		if child.Kind() != "parameter_declaration" {
			return
		}
		for _, nameNode := range IdentifierNodes(child.ChildByFieldName("name"), source) {
			variable := strings.TrimSpace(shared.NodeText(nameNode, source))
			if variable == "" {
				continue
			}
			bindings = append(bindings, LocalNameBinding{
				variable:   variable,
				line:       shared.NodeLine(node),
				scopeStart: shared.NodeLine(body),
				scopeEnd:   shared.NodeEndLine(body),
			})
		}
	})
	return bindings
}

// LocalNameBindingsFromNames scopes local declarations to their nearest
// lexical block or statement.
func LocalNameBindingsFromNames(
	node *tree_sitter.Node,
	nameNodes []*tree_sitter.Node,
	source []byte,
	lookup *ParentLookup,
) []LocalNameBinding {
	scope := NearestLexicalScope(node, lookup)
	if scope == nil {
		return nil
	}
	bindings := make([]LocalNameBinding, 0, len(nameNodes))
	for _, nameNode := range nameNodes {
		variable := strings.TrimSpace(shared.NodeText(nameNode, source))
		if variable == "" {
			continue
		}
		bindings = append(bindings, LocalNameBinding{
			variable:   variable,
			line:       shared.NodeLine(node),
			scopeStart: shared.NodeLine(scope),
			scopeEnd:   shared.NodeEndLine(scope),
		})
	}
	return bindings
}

// LocalReceiverBindings records local receiver type evidence from parameters
// and constructor-return assignments.
func LocalReceiverBindings(
	root *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
	lookup *ParentLookup,
) []LocalReceiverBinding {
	bindings := make([]LocalReceiverBinding, 0)
	mapValueTypes, localInterfaces := goCollectLocalMapValueTypesAndInterfaceNames(root, source, lookup)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			bindings = append(bindings, goLocalReceiverBindingsFromParameters(node, source, localInterfaces)...)
		case "short_var_declaration", "assignment_statement":
			bindings = append(bindings, goLocalReceiverBindingsFromAssignment(node, source, constructorReturns, localInterfaces, lookup)...)
		case "var_spec":
			bindings = append(bindings, goLocalReceiverBindingsFromVarSpec(node, source, constructorReturns, localInterfaces, lookup)...)
		case "range_clause", "for_statement":
			bindings = append(bindings, goLocalReceiverBindingsFromRangeClause(node, source, mapValueTypes, localInterfaces, lookup)...)
		}
	})
	return bindings
}

func goLocalReceiverBindingsFromParameters(
	node *tree_sitter.Node,
	source []byte,
	localInterfaces map[string]struct{},
) []LocalReceiverBinding {
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	parameters := node.ChildByFieldName("parameters")
	if parameters == nil {
		return nil
	}
	bindings := make([]LocalReceiverBinding, 0)
	WalkDirectNamed(parameters, func(child *tree_sitter.Node) {
		if child.Kind() != "parameter_declaration" {
			return
		}
		typeName := TypeNameFromNode(child.ChildByFieldName("type"), source)
		if typeName == "" {
			return
		}
		for _, nameNode := range IdentifierNodes(child.ChildByFieldName("name"), source) {
			variable := strings.TrimSpace(shared.NodeText(nameNode, source))
			if variable == "" {
				continue
			}
			bindings = append(bindings, LocalReceiverBinding{
				Variable:   variable,
				typeName:   typeName,
				concrete:   !goTypeNameIsLocalInterface(typeName, localInterfaces),
				line:       shared.NodeLine(node),
				scopeStart: shared.NodeLine(body),
				scopeEnd:   shared.NodeEndLine(body),
			})
		}
	})
	return bindings
}

func goLocalReceiverBindingsFromAssignment(
	node *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
	localInterfaces map[string]struct{},
	lookup *ParentLookup,
) []LocalReceiverBinding {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	names := AssignableIdentifierNodes(left, source)
	values := ExpressionNodes(right)
	if len(names) == 0 || len(values) == 0 {
		return nil
	}
	count := len(names)
	if len(values) < count {
		count = len(values)
	}
	bindings := make([]LocalReceiverBinding, 0, count)
	for i := 0; i < count; i++ {
		typeName := goConcreteReceiverTypeFromExpression(values[i], source, constructorReturns)
		concrete := typeName != "" && !goTypeNameIsLocalInterface(typeName, localInterfaces)
		if binding := NewLocalReceiverBinding(node, names[i], typeName, concrete, source, lookup); binding.Variable != "" {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func goLocalReceiverBindingsFromVarSpec(
	node *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
	localInterfaces map[string]struct{},
	lookup *ParentLookup,
) []LocalReceiverBinding {
	nameNodes := IdentifierNodes(node.ChildByFieldName("name"), source)
	valueNodes := ExpressionNodes(node.ChildByFieldName("value"))
	if len(nameNodes) == 0 || len(valueNodes) == 0 {
		return nil
	}
	count := len(nameNodes)
	if len(valueNodes) < count {
		count = len(valueNodes)
	}
	bindings := make([]LocalReceiverBinding, 0, count)
	for i := 0; i < count; i++ {
		typeName := goConcreteReceiverTypeFromExpression(valueNodes[i], source, constructorReturns)
		concrete := typeName != "" && !goTypeNameIsLocalInterface(typeName, localInterfaces)
		if binding := NewLocalReceiverBinding(node, nameNodes[i], typeName, concrete, source, lookup); binding.Variable != "" {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

// NewLocalReceiverBinding builds a LocalReceiverBinding scoped to the
// nearest enclosing lexical scope of node, or the zero value (with an empty
// variable name) when node has no enclosing scope.
func NewLocalReceiverBinding(
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	typeName string,
	concrete bool,
	source []byte,
	lookup *ParentLookup,
) LocalReceiverBinding {
	scope := NearestLexicalScope(node, lookup)
	if scope == nil {
		return LocalReceiverBinding{}
	}
	return LocalReceiverBinding{
		Variable:   strings.TrimSpace(shared.NodeText(nameNode, source)),
		typeName:   typeName,
		concrete:   concrete,
		line:       shared.NodeLine(node),
		scopeStart: shared.NodeLine(scope),
		scopeEnd:   shared.NodeEndLine(scope),
	}
}

// InferredReceiverType returns the type most recently bound to receiver at
// or before callLine among bindings whose scope contains callLine, breaking
// ties toward the narrowest enclosing scope. It returns "" when no binding
// applies.
func InferredReceiverType(
	receiver string,
	callLine int,
	bindings []LocalReceiverBinding,
) string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" || callLine <= 0 {
		return ""
	}
	var best LocalReceiverBinding
	for _, binding := range bindings {
		if binding.Variable != receiver ||
			binding.typeName == "" ||
			binding.line > callLine ||
			callLine < binding.scopeStart ||
			callLine > binding.scopeEnd {
			continue
		}
		if best.typeName == "" || binding.line > best.line || spanWidthForGoBinding(binding) < spanWidthForGoBinding(best) {
			best = binding
		}
	}
	return best.typeName
}

// ConcreteInferredReceiverType returns the concrete type bound to receiver
// among the narrowest-scoped bindings applicable at callLine, but only when
// exactly one concrete type is observed among them and no wider (interface
// or unresolved) rebinding of receiver occurs after the last concrete one.
// It returns "" when that single-concrete-type invariant does not hold.
func ConcreteInferredReceiverType(
	receiver string,
	callLine int,
	bindings []LocalReceiverBinding,
) string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" || callLine <= 0 {
		return ""
	}
	applicable := make([]LocalReceiverBinding, 0)
	minSpan := 0
	for _, binding := range bindings {
		if binding.Variable != receiver ||
			binding.line > callLine ||
			callLine < binding.scopeStart ||
			callLine > binding.scopeEnd {
			continue
		}
		span := spanWidthForGoBinding(binding)
		if minSpan == 0 || span < minSpan {
			minSpan = span
			applicable = applicable[:0]
		}
		if span == minSpan {
			applicable = append(applicable, binding)
		}
	}
	var inferred string
	seenConcrete := make(map[string]struct{})
	lastConcreteLine := 0
	lastNonConcreteLine := 0
	for _, binding := range applicable {
		if !binding.concrete || binding.typeName == "" {
			if binding.line > lastNonConcreteLine {
				lastNonConcreteLine = binding.line
			}
			continue
		}
		seenConcrete[binding.typeName] = struct{}{}
		if binding.line > lastConcreteLine {
			inferred = binding.typeName
			lastConcreteLine = binding.line
		}
	}
	if len(seenConcrete) != 1 || lastNonConcreteLine > lastConcreteLine {
		return ""
	}
	return inferred
}

func spanWidthForGoBinding(binding LocalReceiverBinding) int {
	return binding.scopeEnd - binding.scopeStart
}

func goConstructorTypeFromExpression(
	node *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
) string {
	if node == nil || node.Kind() != "call_expression" {
		return ""
	}
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "identifier" {
		return ""
	}
	return constructorReturns[strings.TrimSpace(shared.NodeText(functionNode, source))]
}

// TypeNameFromNode returns the normalized type name for a type-annotated
// node, unwrapping pointer, array, and slice wrappers.
func TypeNameFromNode(node *tree_sitter.Node, source []byte) string {
	if node != nil {
		switch node.Kind() {
		case "type_identifier", "qualified_type", "generic_type":
			return NormalizeTypeName(shared.NodeText(node, source))
		}
	}
	typeNode := FirstNamedDescendant(
		node,
		"type_identifier",
		"qualified_type",
		"generic_type",
		"pointer_type",
		"array_type",
		"slice_type",
	)
	return NormalizeTypeName(shared.NodeText(typeNode, source))
}

// NormalizeTypeName strips pointer, array, slice, generic-instantiation, and
// package-qualifier decoration from a type name's source text, returning the
// bare element type name.
func NormalizeTypeName(value string) string {
	value = strings.TrimSpace(value)
	for {
		trimmed := strings.TrimSpace(strings.TrimPrefix(value, "*"))
		switch {
		case strings.HasPrefix(trimmed, "[]"):
			value = strings.TrimSpace(trimmed[2:])
		case strings.HasPrefix(trimmed, "["):
			closeIndex := strings.Index(trimmed, "]")
			if closeIndex <= 0 {
				value = trimmed
				goto done
			}
			value = strings.TrimSpace(trimmed[closeIndex+1:])
		default:
			value = trimmed
			goto done
		}
	}
done:
	if index := strings.LastIndex(value, "."); index >= 0 {
		value = value[index+1:]
	}
	if index := strings.Index(value, "["); index > 0 {
		value = value[:index]
	}
	value = strings.Trim(value, "[]")
	return strings.TrimSpace(value)
}

// IdentifierNodes returns node itself when it is a non-blank identifier, or
// each non-blank identifier among node's direct named children otherwise
// (for an expression_list or parameter_list of names).
func IdentifierNodes(node *tree_sitter.Node, source []byte) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "identifier" && strings.TrimSpace(shared.NodeText(node, source)) != "_" {
		return []*tree_sitter.Node{node}
	}
	nodes := make([]*tree_sitter.Node, 0)
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "identifier" || strings.TrimSpace(shared.NodeText(&child, source)) == "_" {
			continue
		}
		nodes = append(nodes, &child)
	}
	return nodes
}

// ExpressionNodes returns node itself when it is not an expression_list or
// parameter_list, or each of its direct named children otherwise.
func ExpressionNodes(node *tree_sitter.Node) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() != "expression_list" && node.Kind() != "parameter_list" {
		return []*tree_sitter.Node{node}
	}
	nodes := make([]*tree_sitter.Node, 0)
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		nodes = append(nodes, &child)
	}
	return nodes
}

func goEnclosingFunctionScope(node *tree_sitter.Node, lookup *ParentLookup) *tree_sitter.Node {
	for current := node; current != nil; current = lookup.Parent(current) {
		switch current.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			return current
		}
	}
	return nil
}

// NearestLexicalScope returns the smallest syntax scope that can bound a
// local declaration without making inner-block bindings visible outside. The
// lookup amortizes ancestor traversal to O(1) per step (see #161).
func NearestLexicalScope(node *tree_sitter.Node, lookup *ParentLookup) *tree_sitter.Node {
	for current := node; current != nil; current = lookup.Parent(current) {
		switch current.Kind() {
		case "block", "if_statement", "for_statement", "communication_case", "expression_case", "default_case":
			return current
		}
	}
	return goEnclosingFunctionScope(node, lookup)
}
