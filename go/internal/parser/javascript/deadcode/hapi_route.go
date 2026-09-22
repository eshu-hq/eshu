// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptIsHapiRouteConfigHandler(node *tree_sitter.Node, name string, source []byte, parents *syntax.ParentLookup) bool {
	if node == nil || node.Kind() != "pair" || strings.TrimSpace(name) != "handler" {
		return false
	}
	if !syntax.IsFunctionValue(node.ChildByFieldName("value")) {
		return false
	}
	routeConfigObject := parents.Parent(node)
	if routeConfigObject == nil || routeConfigObject.Kind() != "object" {
		return false
	}
	return javaScriptObjectIsCommonJSExported(routeConfigObject, source, parents) ||
		javaScriptObjectIsInHapiServerRoute(routeConfigObject, source, parents) ||
		javaScriptObjectIsInCommonJSExportedHapiRouteCollection(routeConfigObject, source, parents)
}

// HapiRouteHandlerReferenceCall returns the call record for a Hapi route handler
// property that names an existing function instead of defining one inline, so the
// referenced handler is not reported as dead. It returns nil unless node is a pair
// keyed "handler" whose value is a bare reference and whose enclosing route-config
// object is reachable from a CommonJS export, a server.route call, or an exported
// route collection.
func HapiRouteHandlerReferenceCall(
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	valueNode *tree_sitter.Node,
	source []byte,
	lang string,
	evidence Evidence,
) map[string]any {
	if node == nil || node.Kind() != "pair" {
		return nil
	}
	if strings.TrimSpace(shared.NodeText(nameNode, source)) != "handler" {
		return nil
	}
	if !javaScriptRouteHandlerReferenceValue(valueNode) {
		return nil
	}
	routeConfigObject := evidence.Parents.Parent(node)
	if routeConfigObject == nil || routeConfigObject.Kind() != "object" {
		return nil
	}
	if (!evidence.hapiControllerFile || !javaScriptObjectIsCommonJSExported(routeConfigObject, source, evidence.Parents)) &&
		!javaScriptObjectIsInHapiServerRoute(routeConfigObject, source, evidence.Parents) &&
		!javaScriptObjectIsInCommonJSExportedHapiRouteCollection(routeConfigObject, source, evidence.Parents) {
		return nil
	}
	fullName := strings.TrimSpace(shared.NodeText(valueNode, source))
	name := syntax.CallName(valueNode, source)
	if name == "" {
		name = syntax.IdentifierName(valueNode, source)
	}
	if name == "" || fullName == "" {
		return nil
	}
	return map[string]any{
		"name":        name,
		"full_name":   fullName,
		"call_kind":   "javascript.hapi_route_handler_reference",
		"line_number": shared.NodeLine(valueNode),
		"lang":        lang,
	}
}

func javaScriptRouteHandlerReferenceValue(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "member_expression":
		return true
	default:
		return false
	}
}

func javaScriptObjectIsInHapiServerRoute(objectNode *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) bool {
	routeObject := javaScriptHapiRouteObject(objectNode, source, parents)
	if routeObject == nil {
		return false
	}
	for current := routeObject; current != nil; current = parents.Parent(current) {
		if current.Kind() != "call_expression" {
			continue
		}
		functionNode := current.ChildByFieldName("function")
		_, property, ok := syntax.MemberBaseAndProperty(functionNode, source)
		return ok && strings.EqualFold(property, "route")
	}
	return false
}

func javaScriptObjectIsInCommonJSExportedHapiRouteCollection(objectNode *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) bool {
	routeObject := javaScriptHapiRouteObject(objectNode, source, parents)
	if routeObject == nil {
		return false
	}
	collection := parents.Parent(routeObject)
	if collection == nil || collection.Kind() != "array" {
		return false
	}
	if javaScriptNodeIsCommonJSExportedValue(collection, source, parents) {
		return true
	}
	collectionName := javaScriptVariableNameForValue(collection, source, parents)
	if collectionName == "" {
		return false
	}
	return javaScriptRootExportsIdentifier(collection, collectionName, source, parents)
}

func javaScriptNodeIsCommonJSExportedValue(valueNode *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) bool {
	if valueNode == nil {
		return false
	}
	parent := parents.Parent(valueNode)
	if parent == nil || parent.Kind() != "assignment_expression" {
		return false
	}
	if !syntax.NodeSameRange(parent.ChildByFieldName("right"), valueNode) {
		return false
	}
	return javaScriptCommonJSAssignmentTarget(parent.ChildByFieldName("left"), source)
}

func javaScriptVariableNameForValue(valueNode *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) string {
	if valueNode == nil {
		return ""
	}
	parent := parents.Parent(valueNode)
	if parent == nil || parent.Kind() != "variable_declarator" {
		return ""
	}
	if !syntax.NodeSameRange(parent.ChildByFieldName("value"), valueNode) {
		return ""
	}
	return syntax.IdentifierName(parent.ChildByFieldName("name"), source)
}

func javaScriptRootExportsIdentifier(node *tree_sitter.Node, name string, source []byte, parents *syntax.ParentLookup) bool {
	name = strings.TrimSpace(name)
	if node == nil || name == "" {
		return false
	}
	root := node
	for parents.Parent(root) != nil {
		root = parents.Parent(root)
	}
	found := false
	shared.WalkNamed(root, func(candidate *tree_sitter.Node) {
		if found || candidate.Kind() != "assignment_expression" {
			return
		}
		rightNode := candidate.ChildByFieldName("right")
		if syntax.IdentifierName(rightNode, source) != name {
			return
		}
		if javaScriptCommonJSAssignmentTarget(candidate.ChildByFieldName("left"), source) {
			found = true
		}
	})
	return found
}

func javaScriptHapiRouteObject(objectNode *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) *tree_sitter.Node {
	if objectNode == nil || objectNode.Kind() != "object" {
		return nil
	}
	if javaScriptObjectHasPairKey(objectNode, source, "method") && javaScriptObjectHasPairKey(objectNode, source, "path") {
		return objectNode
	}
	parent := parents.Parent(objectNode)
	if parent == nil || parent.Kind() != "pair" {
		return nil
	}
	switch strings.TrimSpace(shared.NodeText(parent.ChildByFieldName("key"), source)) {
	case "config", "options":
	default:
		return nil
	}
	routeObject := parents.Parent(parent)
	if routeObject == nil || routeObject.Kind() != "object" {
		return nil
	}
	if javaScriptObjectHasPairKey(routeObject, source, "method") && javaScriptObjectHasPairKey(routeObject, source, "path") {
		return routeObject
	}
	return nil
}

func javaScriptObjectHasPairKey(objectNode *tree_sitter.Node, source []byte, key string) bool {
	if objectNode == nil || objectNode.Kind() != "object" {
		return false
	}
	cursor := objectNode.Walk()
	defer cursor.Close()
	for _, child := range objectNode.NamedChildren(cursor) {
		child := child
		if child.Kind() != "pair" {
			continue
		}
		if strings.TrimSpace(shared.NodeText(child.ChildByFieldName("key"), source)) == key {
			return true
		}
	}
	return false
}

func javaScriptObjectIsCommonJSExported(objectNode *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) bool {
	for current := objectNode; current != nil; current = parents.Parent(current) {
		if current.Kind() != "object" {
			continue
		}
		parent := parents.Parent(current)
		if parent == nil {
			continue
		}
		switch parent.Kind() {
		case "assignment_expression":
			if !syntax.NodeSameRange(parent.ChildByFieldName("right"), current) {
				continue
			}
			return javaScriptCommonJSAssignmentTarget(parent.ChildByFieldName("left"), source)
		case "export_statement":
			return true
		}
	}
	return false
}

func javaScriptHapiPluginRegisterAliasRootKinds(
	root *tree_sitter.Node,
	source []byte,
) map[string][]string {
	registered := make(map[string][]string)
	if root == nil {
		return registered
	}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		leftNode := node.ChildByFieldName("left")
		if CommonJSExportName(leftNode, source) != "plugin" {
			return
		}
		objectNode := node.ChildByFieldName("right")
		if objectNode == nil || objectNode.Kind() != "object" {
			return
		}
		for _, name := range javaScriptHapiPluginRegisterAliasNames(objectNode, source) {
			registered[strings.ToLower(name)] = shared.AppendUniqueString(
				registered[strings.ToLower(name)],
				"javascript.hapi_plugin_register",
			)
		}
	})
	return registered
}

func javaScriptHapiPluginRegisterAliasNames(objectNode *tree_sitter.Node, source []byte) []string {
	names := make([]string, 0, 1)
	cursor := objectNode.Walk()
	defer cursor.Close()
	for _, child := range objectNode.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "pair":
			key := strings.TrimSpace(shared.NodeText(child.ChildByFieldName("key"), source))
			if key != "register" {
				continue
			}
			valueNode := child.ChildByFieldName("value")
			if name := syntax.IdentifierName(valueNode, source); name != "" {
				names = shared.AppendUniqueString(names, name)
			}
		case "shorthand_property_identifier", "identifier", "property_identifier":
			name := strings.TrimSpace(shared.NodeText(&child, source))
			if name == "register" {
				names = shared.AppendUniqueString(names, name)
			}
		}
	}
	return names
}

func javaScriptDefaultObjectExportAliasRootKinds(
	root *tree_sitter.Node,
	source []byte,
	key string,
	rootKind string,
) map[string][]string {
	return javaScriptObjectExportAliasRootKinds(root, source, "export default", key, rootKind)
}

func javaScriptTypeScriptExportAssignmentAliasRootKinds(
	root *tree_sitter.Node,
	source []byte,
	rootKind string,
) map[string][]string {
	return javaScriptObjectExportAliasRootKinds(root, source, "export =", "", rootKind)
}

func javaScriptObjectExportAliasRootKinds(
	root *tree_sitter.Node,
	source []byte,
	exportPrefix string,
	key string,
	rootKind string,
) map[string][]string {
	registered := make(map[string][]string)
	if root == nil || strings.TrimSpace(exportPrefix) == "" || strings.TrimSpace(rootKind) == "" {
		return registered
	}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "export_statement" {
			return
		}
		if !strings.HasPrefix(strings.TrimSpace(shared.NodeText(node, source)), exportPrefix) {
			return
		}
		objectNode := javaScriptFirstNamedDescendantOfKind(node, "object")
		if objectNode == nil {
			return
		}
		for _, name := range javaScriptObjectAliasNames(objectNode, source, key) {
			registered[strings.ToLower(name)] = shared.AppendUniqueString(registered[strings.ToLower(name)], rootKind)
		}
	})
	return registered
}

func javaScriptObjectAliasNames(objectNode *tree_sitter.Node, source []byte, keyFilter string) []string {
	names := make([]string, 0, 4)
	cursor := objectNode.Walk()
	defer cursor.Close()
	for _, child := range objectNode.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "pair":
			key := strings.Trim(strings.TrimSpace(shared.NodeText(child.ChildByFieldName("key"), source)), `"'`)
			if keyFilter != "" && key != keyFilter {
				continue
			}
			valueNode := child.ChildByFieldName("value")
			if name := syntax.IdentifierName(valueNode, source); name != "" {
				names = shared.AppendUniqueString(names, name)
			}
		case "shorthand_property_identifier", "identifier", "property_identifier":
			name := strings.TrimSpace(shared.NodeText(&child, source))
			if keyFilter != "" && name != keyFilter {
				continue
			}
			names = shared.AppendUniqueString(names, name)
		}
	}
	return names
}

func javaScriptFirstNamedDescendantOfKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	if node == nil || strings.TrimSpace(kind) == "" {
		return nil
	}
	if node.Kind() == kind {
		return shared.CloneNode(node)
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if got := javaScriptFirstNamedDescendantOfKind(&child, kind); got != nil {
			return got
		}
	}
	return nil
}

func javaScriptPairInsideCommonJSPluginObject(node *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) bool {
	if node == nil || node.Kind() != "pair" {
		return false
	}
	objectNode := parents.Parent(node)
	if objectNode == nil || objectNode.Kind() != "object" {
		return false
	}
	parent := parents.Parent(objectNode)
	if parent == nil || parent.Kind() != "assignment_expression" ||
		!syntax.NodeSameRange(parent.ChildByFieldName("right"), objectNode) {
		return false
	}
	return CommonJSExportName(parent.ChildByFieldName("left"), source) == "plugin"
}

func javaScriptCommonJSAssignmentTarget(node *tree_sitter.Node, source []byte) bool {
	if node == nil {
		return false
	}
	switch strings.TrimSpace(shared.NodeText(node, source)) {
	case "module.exports", "exports":
		return true
	}
	return CommonJSExportName(node, source) != ""
}
