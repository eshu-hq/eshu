// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// emitPHPVariableName emits a deduplicated variable row for a variable_name
// node, resolving its type from property declarations, parameters, or the
// nearest assignment, and recording assignment types for later receiver
// inference.
func emitPHPVariableName(state *phpParseState, node *tree_sitter.Node) {
	variable := strings.TrimSpace(shared.NodeText(node, state.source))
	if variable == "" || variable == "$this" {
		return
	}

	ctx := phpResolveContext(node, state.source, state.parents)
	bareName := strings.TrimPrefix(variable, "$")
	variableType := state.resolveVariableType(node, variable, bareName, ctx)

	// Record assignment and property types so receiver inference resolves later
	// uses of this variable even when its first textual occurrence is filtered.
	if ctx.kind == "class_declaration" {
		if state.classPropertyTypes[ctx.name] == nil {
			state.classPropertyTypes[ctx.name] = make(map[string]string)
		}
		if _, recorded := state.classPropertyTypes[ctx.name][bareName]; !recorded {
			state.classPropertyTypes[ctx.name][bareName] = variableType
		}
	}
	if scopeKey := phpScopeKeyForNode(node, state.source, state.parents); scopeKey != "" && variableType != "" && variableType != "mixed" {
		if state.localVariableTypes[scopeKey] == nil {
			state.localVariableTypes[scopeKey] = make(map[string]string)
		}
		if _, recorded := state.localVariableTypes[scopeKey][bareName]; !recorded {
			state.localVariableTypes[scopeKey][bareName] = variableType
		}
	}

	if _, ok := state.seenVariables[variable]; ok {
		return
	}
	state.seenVariables[variable] = struct{}{}

	item := map[string]any{
		"name":        variable,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeLine(node),
		"lang":        "php",
		"type":        variableType,
	}
	if ctx.name != "" {
		item["context"] = ctx.name
	}
	switch ctx.kind {
	case "class_declaration", "interface_declaration", "trait_declaration":
		item["class_context"] = ctx.name
	default:
		item["class_context"] = nil
	}
	shared.AppendBucket(state.payload, "variables", item)
}

// resolveVariableType determines the static type for a variable occurrence using
// declared property types, the local scope variable map, the enclosing
// assignment right-hand side, and parameter types. It returns "mixed" when no
// stronger evidence is available.
func (state *phpParseState) resolveVariableType(
	node *tree_sitter.Node,
	variable string,
	bareName string,
	ctx phpCallContext,
) string {
	if ctx.kind == "class_declaration" {
		if declared := state.classPropertyTypes[ctx.name][bareName]; declared != "" {
			return declared
		}
	}

	scopeKey := phpScopeKeyForNode(node, state.source, state.parents)
	if scopeKey != "" {
		if known := state.localVariableTypes[scopeKey][bareName]; known != "" {
			return known
		}
	}

	if assigned := state.assignmentRHSType(node, variable, scopeKey); assigned != "" {
		return assigned
	}

	return "mixed"
}

// assignmentRHSType infers the type assigned to a variable from the nearest
// enclosing assignment whose left-hand side is the variable itself.
func (state *phpParseState) assignmentRHSType(node *tree_sitter.Node, variable string, scopeKey string) string {
	assignment := phpEnclosingAssignment(node)
	if assignment == nil {
		return ""
	}
	leftNode := assignment.ChildByFieldName("left")
	rightNode := assignment.ChildByFieldName("right")
	if leftNode == nil || rightNode == nil {
		return ""
	}
	if strings.TrimSpace(shared.NodeText(leftNode, state.source)) != variable {
		return ""
	}

	if rightNode.Kind() == "object_creation_expression" {
		if name := phpAnonymousAssignmentType(rightNode); name != "" {
			return name
		}
	}

	expr := phpNormalizeNullsafe(shared.NodeText(rightNode, state.source))
	classContext := phpNearestTypeContext(node, state.source, state.parents)
	if inferred := inferPHPReferenceType(
		expr,
		classContext,
		state.classParentTypes,
		state.classPropertyTypes,
		state.localVariableTypes[scopeKey],
		state.methodReturnTypes,
		state.functionReturnTypes,
		state.importAliases,
	); inferred != "" {
		return inferred
	}
	if inferred := inferPHPFunctionCallType(expr, state.functionReturnTypes, state.importAliases); inferred != "" {
		return inferred
	}
	return ""
}

// phpAnonymousAssignmentType returns the synthetic anonymous-class name when an
// object creation expression instantiates an anonymous class.
func phpAnonymousAssignmentType(node *tree_sitter.Node) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "anonymous_class" {
			return phpAnonymousClassName(shared.NodeLine(&child))
		}
	}
	return ""
}

// phpEnclosingAssignment returns the nearest assignment_expression ancestor of a
// node, stopping at statement boundaries.
func phpEnclosingAssignment(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "assignment_expression":
			return current
		case "expression_statement", "compound_statement", "method_declaration", "function_definition":
			return nil
		}
	}
	return nil
}
