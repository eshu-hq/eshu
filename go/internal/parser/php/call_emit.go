// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpCallContext carries the resolved enclosing function/method or type context
// for a call or variable row.
type phpCallContext struct {
	name string
	kind string
	line int
}

// phpResolveContext returns the nearest enclosing function, method, or type
// declaration for a node, mirroring the legacy scope-stack context contract.
func phpResolveContext(node *tree_sitter.Node, source []byte, parents *phpParentLookup) phpCallContext {
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "function_definition", "method_declaration":
			nameNode := current.ChildByFieldName("name")
			return phpCallContext{
				name: strings.TrimSpace(shared.NodeText(nameNode, source)),
				kind: phpFunctionContextKind(current),
				line: shared.NodeLine(phpNameNode(current)),
			}
		case "class_declaration", "interface_declaration", "trait_declaration":
			nameNode := current.ChildByFieldName("name")
			return phpCallContext{
				name: strings.TrimSpace(shared.NodeText(nameNode, source)),
				kind: current.Kind(),
				line: shared.NodeLine(phpNameNode(current)),
			}
		}
	}
	return phpCallContext{}
}

// phpFunctionContextKind reports whether a function node is a free function or a
// method based on its enclosing declaration list.
func phpFunctionContextKind(node *tree_sitter.Node) string {
	if node.Kind() == "method_declaration" {
		return "method_declaration"
	}
	return "function_definition"
}

// phpNearestTypeContext returns the nearest enclosing class, interface, or trait
// name for receiver inference of self/static/parent and $this chains.
func phpNearestTypeContext(node *tree_sitter.Node, source []byte, parents *phpParentLookup) string {
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "trait_declaration":
			return strings.TrimSpace(shared.NodeText(current.ChildByFieldName("name"), source))
		case "anonymous_class":
			return phpAnonymousClassName(shared.NodeLine(current))
		}
	}
	return ""
}

// phpScopeKeyForNode returns the local-variable scope key for the function or
// method that encloses a node, or the empty string outside any function.
func phpScopeKeyForNode(node *tree_sitter.Node, source []byte, parents *phpParentLookup) string {
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "function_definition", "method_declaration":
			functionName := strings.TrimSpace(shared.NodeText(current.ChildByFieldName("name"), source))
			typeName := phpNearestTypeContext(current, source, parents)
			return phpFunctionScopeKey(typeName, functionName)
		}
	}
	return ""
}

// emitPHPMemberCall emits a call row for instance and nullsafe method calls,
// reconstructing the receiver chain text and inferring the receiver type.
func emitPHPMemberCall(state *phpParseState, node *tree_sitter.Node) {
	objectNode := node.ChildByFieldName("object")
	nameNode := node.ChildByFieldName("name")
	if objectNode == nil || nameNode == nil {
		return
	}
	methodName := strings.TrimSpace(shared.NodeText(nameNode, state.source))
	if methodName == "" {
		return
	}
	receiverText := phpNormalizeNullsafe(shared.NodeText(objectNode, state.source))
	chainExpr := receiverText + "->" + methodName
	fullName := normalizePHPMethodCall(chainExpr)
	ctx := phpResolveContext(node, state.source, state.parents)
	inferred := state.inferReceiverType(receiverText, node)
	appendUniquePHPCall(
		state.payload, state.seenCalls,
		methodName, fullName, shared.NodeLine(node),
		phpCallArguments(node, state.source),
		ctx.name, ctx.kind, ctx.line, inferred,
	)
}

// emitPHPScopedCall emits a call row for static and self/parent/static method
// calls of the form Receiver::method(...).
func emitPHPScopedCall(state *phpParseState, node *tree_sitter.Node) {
	scopeNode := node.ChildByFieldName("scope")
	nameNode := node.ChildByFieldName("name")
	if scopeNode == nil || nameNode == nil {
		return
	}
	methodName := strings.TrimSpace(shared.NodeText(nameNode, state.source))
	if methodName == "" {
		return
	}
	classContext := phpNearestTypeContext(node, state.source, state.parents)
	receiver := normalizePHPStaticReceiver(
		shared.NodeText(scopeNode, state.source),
		classContext,
		state.classParentTypes,
		state.importAliases,
	)
	if receiver == "" {
		return
	}
	fullName := receiver + "." + methodName
	ctx := phpResolveContext(node, state.source, state.parents)
	appendUniquePHPCall(
		state.payload, state.seenCalls,
		methodName, fullName, shared.NodeLine(node),
		phpCallArguments(node, state.source),
		ctx.name, ctx.kind, ctx.line, receiver,
	)
}

// emitPHPObjectCreation emits a call row for `new Type(...)` expressions.
func emitPHPObjectCreation(state *phpParseState, node *tree_sitter.Node) {
	classNode := phpObjectCreationTypeNode(node)
	if classNode == nil {
		return
	}
	rawName := strings.TrimSpace(shared.NodeText(classNode, state.source))
	className := shared.LastPathSegment(rawName, `\`)
	if className == "" {
		return
	}
	ctx := phpResolveContext(node, state.source, state.parents)
	appendUniquePHPCall(
		state.payload, state.seenCalls,
		className, className, shared.NodeLine(node),
		phpCallArguments(node, state.source),
		ctx.name, ctx.kind, ctx.line,
		normalizePHPImportedTypeName(className, state.importAliases),
	)
}

// emitPHPFunctionCall emits a call row for a free function call when the callee
// is a bare name, skipping language constructs handled elsewhere.
func emitPHPFunctionCall(state *phpParseState, node *tree_sitter.Node) {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "name" {
		return
	}
	name := strings.TrimSpace(shared.NodeText(functionNode, state.source))
	if name == "" {
		return
	}
	ctx := phpResolveContext(node, state.source, state.parents)
	appendUniquePHPCall(
		state.payload, state.seenCalls,
		name, name, shared.NodeLine(node),
		phpCallArguments(node, state.source),
		ctx.name, ctx.kind, ctx.line, "",
	)
}

// inferReceiverType resolves the static type of a member-call receiver
// expression using the file's accumulated property, parent, variable, and
// return-type evidence.
func (state *phpParseState) inferReceiverType(receiverText string, node *tree_sitter.Node) string {
	classContext := phpNearestTypeContext(node, state.source, state.parents)
	scopeKey := phpScopeKeyForNode(node, state.source, state.parents)
	return inferPHPReferenceType(
		receiverText,
		classContext,
		state.classParentTypes,
		state.classPropertyTypes,
		state.localVariableTypes[scopeKey],
		state.methodReturnTypes,
		state.functionReturnTypes,
		state.importAliases,
	)
}

// phpCallArguments returns the raw source text of each argument in a call's
// arguments node, preserving multiline formatting.
func phpCallArguments(node *tree_sitter.Node, source []byte) []string {
	argsNode := phpArgumentsNode(node)
	if argsNode == nil {
		return nil
	}
	args := make([]string, 0)
	cursor := argsNode.Walk()
	defer cursor.Close()
	for _, child := range argsNode.NamedChildren(cursor) {
		child := child
		if child.Kind() != "argument" {
			continue
		}
		if text := strings.TrimSpace(shared.NodeText(&child, source)); text != "" {
			args = append(args, text)
		}
	}
	return args
}

func phpArgumentsNode(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "arguments" {
			return shared.CloneNode(&child)
		}
	}
	return nil
}

func phpObjectCreationTypeNode(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "name", "qualified_name":
			return shared.CloneNode(&child)
		case "relative_scope":
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// phpNormalizeNullsafe replaces nullsafe `?->` operators with `->` so receiver
// chain reconstruction matches the legacy normalized form.
func phpNormalizeNullsafe(text string) string {
	return strings.ReplaceAll(text, "?->", "->")
}
