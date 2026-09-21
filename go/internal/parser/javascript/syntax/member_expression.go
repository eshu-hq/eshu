// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// MemberBaseAndProperty splits a member_expression into its base object name
// and property name. ok is false unless both resolve to non-empty
// identifiers, so callers can treat the pair as a qualified selector.
func MemberBaseAndProperty(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node == nil || node.Kind() != "member_expression" {
		return "", "", false
	}
	objectNode := node.ChildByFieldName("object")
	propertyNode := node.ChildByFieldName("property")
	base := memberExpressionBase(objectNode, source)
	property := IdentifierName(propertyNode, source)
	if base == "" || property == "" {
		return "", "", false
	}
	return base, property, true
}

// memberExpressionBase resolves the base identifier of a member expression
// object, unwrapping an Express `app.route(path)` call so the chain
// `app.route(path).get(...)` reports `app` as the base.
func memberExpressionBase(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if base := IdentifierName(node, source); base != "" {
		return base
	}
	switch node.Kind() {
	case "call_expression":
		functionNode := node.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "member_expression" {
			return ""
		}
		routeBase, routeProperty, ok := MemberBaseAndProperty(functionNode, source)
		if !ok || strings.ToLower(routeProperty) != "route" {
			return ""
		}
		return routeBase
	default:
		return ""
	}
}

// IsExpressRouteChain reports whether node is the member expression of an
// Express `router.route(path).method(...)` chain, where the route path is
// argument 0 rather than a registration path argument.
func IsExpressRouteChain(node *tree_sitter.Node, source []byte) bool {
	if node == nil || node.Kind() != "member_expression" {
		return false
	}
	objectNode := node.ChildByFieldName("object")
	if objectNode == nil || objectNode.Kind() != "call_expression" {
		return false
	}
	functionNode := objectNode.ChildByFieldName("function")
	_, property, ok := MemberBaseAndProperty(functionNode, source)
	return ok && strings.ToLower(property) == "route"
}

// ExpressHandlerNames returns named route callbacks from an Express route
// argument, including handler arrays. Anonymous inline callbacks are not
// roots because the parser has no stable symbol to annotate.
func ExpressHandlerNames(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	if name := IdentifierName(node, source); name != "" {
		return []string{name}
	}
	switch node.Kind() {
	case "array", "parenthesized_expression":
	default:
		return nil
	}

	names := []string{}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		for _, name := range ExpressHandlerNames(&children[i], source) {
			names = shared.AppendUniqueString(names, name)
		}
	}
	return names
}

// IdentifierName returns the trimmed text of an identifier or
// property_identifier node, or the empty string for any other node kind.
func IdentifierName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "property_identifier":
		return strings.TrimSpace(shared.NodeText(node, source))
	default:
		return ""
	}
}
