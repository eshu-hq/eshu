// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpClassBases returns extends and implements base names for a class or
// anonymous class declaration in source order.
func phpClassBases(node *tree_sitter.Node, source []byte) []string {
	var bases []string
	bases = append(bases, phpBaseClauseNames(node, source)...)
	bases = append(bases, phpInterfaceClauseNames(node, source)...)
	bases = append(bases, phpTraitUseNames(node, source)...)
	return dedupePHPNonEmptyStrings(bases)
}

// phpInterfaceBases returns the extends base names for an interface declaration.
func phpInterfaceBases(node *tree_sitter.Node, source []byte) []string {
	return dedupePHPNonEmptyStrings(phpBaseClauseNames(node, source))
}

// phpClassExtendsBase returns the first extends base name for a class so the
// parser can resolve self/parent receiver chains.
func phpClassExtendsBase(node *tree_sitter.Node, source []byte) string {
	names := phpBaseClauseNames(node, source)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func phpBaseClauseNames(node *tree_sitter.Node, source []byte) []string {
	return phpNamesUnderChild(node, "base_clause", source)
}

func phpInterfaceClauseNames(node *tree_sitter.Node, source []byte) []string {
	return phpNamesUnderChild(node, "class_interface_clause", source)
}

// phpTraitUseNames returns the trait names imported by `use Trait;` statements
// inside a class or anonymous class declaration list.
func phpTraitUseNames(node *tree_sitter.Node, source []byte) []string {
	list := phpDeclarationList(node)
	if list == nil {
		return nil
	}
	var names []string
	cursor := list.Walk()
	defer cursor.Close()
	for _, member := range list.NamedChildren(cursor) {
		member := member
		if member.Kind() != "use_declaration" {
			continue
		}
		names = append(names, phpDirectNameChildren(&member, source)...)
	}
	return names
}

// phpNamesUnderChild returns last-segment names of every `name` or
// `qualified_name` node directly under the first child of the given kind.
func phpNamesUnderChild(node *tree_sitter.Node, childKind string, source []byte) []string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != childKind {
			continue
		}
		return phpTypeReferenceNames(&child, source)
	}
	return nil
}

// phpTypeReferenceNames returns last-segment names for the `name` and
// `qualified_name` children of a clause node.
func phpTypeReferenceNames(node *tree_sitter.Node, source []byte) []string {
	var names []string
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "name", "qualified_name":
			if name := shared.LastPathSegment(shared.NodeText(&child, source), `\`); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// phpDirectNameChildren returns last-segment names for direct `name` and
// `qualified_name` children, stopping before nested groups such as use_list.
func phpDirectNameChildren(node *tree_sitter.Node, source []byte) []string {
	var names []string
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "name", "qualified_name":
			if name := shared.LastPathSegment(shared.NodeText(&child, source), `\`); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// phpMemberTypeNode returns the declared type node for a property or parameter
// declaration, skipping modifiers and variable names.
func phpMemberTypeNode(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "primitive_type", "named_type", "optional_type", "union_type", "intersection_type", "bottom_type":
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// phpTypeNodeName returns the normalized type name for a type node, joining
// union members with `|` and resolving nullable and named types.
func phpTypeNodeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return normalizePHPTypeName(shared.NodeText(node, source))
}

// phpFunctionReturnType returns the normalized declared return type for a
// function or method declaration, or the empty string when none is declared.
func phpFunctionReturnType(node *tree_sitter.Node, source []byte) string {
	params := phpFormalParameters(node)
	if params == nil {
		return ""
	}
	cursor := node.Walk()
	defer cursor.Close()
	seenParams := false
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "formal_parameters" {
			seenParams = true
			continue
		}
		if !seenParams {
			continue
		}
		switch child.Kind() {
		case "primitive_type", "named_type", "optional_type", "union_type", "intersection_type", "bottom_type":
			return normalizePHPTypeName(shared.NodeText(&child, source))
		case "compound_statement":
			return ""
		}
	}
	return ""
}

// phpFunctionParameterNames returns the `$name` tokens for each declared
// parameter in source order.
func phpFunctionParameterNames(node *tree_sitter.Node, source []byte) []string {
	params := phpFormalParameters(node)
	if params == nil {
		return []string{}
	}
	names := make([]string, 0)
	cursor := params.Walk()
	defer cursor.Close()
	for _, param := range params.NamedChildren(cursor) {
		param := param
		switch param.Kind() {
		case "simple_parameter", "variadic_parameter", "property_promotion_parameter":
		default:
			continue
		}
		if variable := phpParameterVariableName(&param, source); variable != "" {
			names = append(names, "$"+variable)
		}
	}
	return names
}

// phpParameterVariableName returns the bare parameter name without the leading
// dollar sign.
func phpParameterVariableName(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "variable_name" {
			return strings.TrimPrefix(strings.TrimSpace(shared.NodeText(&child, source)), "$")
		}
	}
	return ""
}

// phpPropertyElementName returns the bare property name for a property_element.
func phpPropertyElementName(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "variable_name" {
			return strings.TrimPrefix(strings.TrimSpace(shared.NodeText(&child, source)), "$")
		}
	}
	return ""
}

// phpMethodIsPublic reports whether a method declaration is public, treating an
// absent visibility modifier as public per PHP semantics.
func phpMethodIsPublic(node *tree_sitter.Node, source []byte) bool {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "visibility_modifier" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(shared.NodeText(&child, source))) {
		case "private", "protected":
			return false
		}
	}
	return true
}

// phpFunctionScopeKey returns the local variable scope key for a function or
// method declaration. Free functions use an empty class context.
func phpFunctionScopeKey(typeName string, functionName string) string {
	return typeName + "::" + functionName
}

// phpSemanticKindForMethod classifies a method name as a magic method when it
// uses the PHP double-underscore convention.
func phpSemanticKindForMethod(name string) string {
	if strings.HasPrefix(name, "__") {
		return "magic_method"
	}
	return ""
}
