package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptIsHapiRouteConfigHandler(node *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) bool {
	if node == nil || node.Kind() != "pair" || strings.TrimSpace(name) != "handler" {
		return false
	}
	if !isJavaScriptFunctionValue(node.ChildByFieldName("value")) {
		return false
	}
	routeConfigObject := parents.parent(node)
	if routeConfigObject == nil || routeConfigObject.Kind() != "object" {
		return false
	}
	return javaScriptObjectIsCommonJSExported(routeConfigObject, source, parents) ||
		javaScriptObjectIsInHapiServerRoute(routeConfigObject, source, parents) ||
		javaScriptObjectIsInCommonJSExportedHapiRouteCollection(routeConfigObject, source, parents)
}

func javaScriptHapiRouteHandlerReferenceCall(
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	valueNode *tree_sitter.Node,
	source []byte,
	lang string,
	evidence javaScriptDeadCodeEvidence,
) map[string]any {
	if node == nil || node.Kind() != "pair" {
		return nil
	}
	if strings.TrimSpace(nodeText(nameNode, source)) != "handler" {
		return nil
	}
	if !javaScriptRouteHandlerReferenceValue(valueNode) {
		return nil
	}
	routeConfigObject := evidence.parents.parent(node)
	if routeConfigObject == nil || routeConfigObject.Kind() != "object" {
		return nil
	}
	if (!evidence.hapiControllerFile || !javaScriptObjectIsCommonJSExported(routeConfigObject, source, evidence.parents)) &&
		!javaScriptObjectIsInHapiServerRoute(routeConfigObject, source, evidence.parents) &&
		!javaScriptObjectIsInCommonJSExportedHapiRouteCollection(routeConfigObject, source, evidence.parents) {
		return nil
	}
	fullName := strings.TrimSpace(nodeText(valueNode, source))
	name := javaScriptCallName(valueNode, source)
	if name == "" {
		name = javaScriptIdentifierName(valueNode, source)
	}
	if name == "" || fullName == "" {
		return nil
	}
	return map[string]any{
		"name":        name,
		"full_name":   fullName,
		"call_kind":   "javascript.hapi_route_handler_reference",
		"line_number": nodeLine(valueNode),
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

func javaScriptObjectIsInHapiServerRoute(objectNode *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) bool {
	routeObject := javaScriptHapiRouteObject(objectNode, source, parents)
	if routeObject == nil {
		return false
	}
	for current := routeObject; current != nil; current = parents.parent(current) {
		if current.Kind() != "call_expression" {
			continue
		}
		functionNode := current.ChildByFieldName("function")
		_, property, ok := javaScriptMemberBaseAndProperty(functionNode, source)
		return ok && strings.EqualFold(property, "route")
	}
	return false
}

func javaScriptObjectIsInCommonJSExportedHapiRouteCollection(objectNode *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) bool {
	routeObject := javaScriptHapiRouteObject(objectNode, source, parents)
	if routeObject == nil {
		return false
	}
	collection := parents.parent(routeObject)
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

func javaScriptNodeIsCommonJSExportedValue(valueNode *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) bool {
	if valueNode == nil {
		return false
	}
	parent := parents.parent(valueNode)
	if parent == nil || parent.Kind() != "assignment_expression" {
		return false
	}
	if !javaScriptNodeSameRange(parent.ChildByFieldName("right"), valueNode) {
		return false
	}
	return javaScriptCommonJSAssignmentTarget(parent.ChildByFieldName("left"), source)
}

func javaScriptVariableNameForValue(valueNode *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) string {
	if valueNode == nil {
		return ""
	}
	parent := parents.parent(valueNode)
	if parent == nil || parent.Kind() != "variable_declarator" {
		return ""
	}
	if !javaScriptNodeSameRange(parent.ChildByFieldName("value"), valueNode) {
		return ""
	}
	return javaScriptIdentifierName(parent.ChildByFieldName("name"), source)
}

func javaScriptRootExportsIdentifier(node *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) bool {
	name = strings.TrimSpace(name)
	if node == nil || name == "" {
		return false
	}
	root := node
	for parents.parent(root) != nil {
		root = parents.parent(root)
	}
	found := false
	walkNamed(root, func(candidate *tree_sitter.Node) {
		if found || candidate.Kind() != "assignment_expression" {
			return
		}
		rightNode := candidate.ChildByFieldName("right")
		if javaScriptIdentifierName(rightNode, source) != name {
			return
		}
		if javaScriptCommonJSAssignmentTarget(candidate.ChildByFieldName("left"), source) {
			found = true
		}
	})
	return found
}

func javaScriptHapiRouteObject(objectNode *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) *tree_sitter.Node {
	if objectNode == nil || objectNode.Kind() != "object" {
		return nil
	}
	if javaScriptObjectHasPairKey(objectNode, source, "method") && javaScriptObjectHasPairKey(objectNode, source, "path") {
		return objectNode
	}
	parent := parents.parent(objectNode)
	if parent == nil || parent.Kind() != "pair" {
		return nil
	}
	switch strings.TrimSpace(nodeText(parent.ChildByFieldName("key"), source)) {
	case "config", "options":
	default:
		return nil
	}
	routeObject := parents.parent(parent)
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
		if strings.TrimSpace(nodeText(child.ChildByFieldName("key"), source)) == key {
			return true
		}
	}
	return false
}

func javaScriptObjectIsCommonJSExported(objectNode *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) bool {
	for current := objectNode; current != nil; current = parents.parent(current) {
		if current.Kind() != "object" {
			continue
		}
		parent := parents.parent(current)
		if parent == nil {
			continue
		}
		switch parent.Kind() {
		case "assignment_expression":
			if !javaScriptNodeSameRange(parent.ChildByFieldName("right"), current) {
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
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		leftNode := node.ChildByFieldName("left")
		if javaScriptCommonJSExportName(leftNode, source) != "plugin" {
			return
		}
		objectNode := node.ChildByFieldName("right")
		if objectNode == nil || objectNode.Kind() != "object" {
			return
		}
		for _, name := range javaScriptHapiPluginRegisterAliasNames(objectNode, source) {
			registered[strings.ToLower(name)] = appendUniqueString(
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
			key := strings.TrimSpace(nodeText(child.ChildByFieldName("key"), source))
			if key != "register" {
				continue
			}
			valueNode := child.ChildByFieldName("value")
			if name := javaScriptIdentifierName(valueNode, source); name != "" {
				names = appendUniqueString(names, name)
			}
		case "shorthand_property_identifier", "identifier", "property_identifier":
			name := strings.TrimSpace(nodeText(&child, source))
			if name == "register" {
				names = appendUniqueString(names, name)
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
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "export_statement" {
			return
		}
		if !strings.HasPrefix(strings.TrimSpace(nodeText(node, source)), exportPrefix) {
			return
		}
		objectNode := javaScriptFirstNamedDescendantOfKind(node, "object")
		if objectNode == nil {
			return
		}
		for _, name := range javaScriptObjectAliasNames(objectNode, source, key) {
			registered[strings.ToLower(name)] = appendUniqueString(registered[strings.ToLower(name)], rootKind)
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
			key := strings.Trim(strings.TrimSpace(nodeText(child.ChildByFieldName("key"), source)), `"'`)
			if keyFilter != "" && key != keyFilter {
				continue
			}
			valueNode := child.ChildByFieldName("value")
			if name := javaScriptIdentifierName(valueNode, source); name != "" {
				names = appendUniqueString(names, name)
			}
		case "shorthand_property_identifier", "identifier", "property_identifier":
			name := strings.TrimSpace(nodeText(&child, source))
			if keyFilter != "" && name != keyFilter {
				continue
			}
			names = appendUniqueString(names, name)
		}
	}
	return names
}

func javaScriptFirstNamedDescendantOfKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	if node == nil || strings.TrimSpace(kind) == "" {
		return nil
	}
	if node.Kind() == kind {
		return cloneNode(node)
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

func javaScriptPairInsideCommonJSPluginObject(node *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) bool {
	if node == nil || node.Kind() != "pair" {
		return false
	}
	objectNode := parents.parent(node)
	if objectNode == nil || objectNode.Kind() != "object" {
		return false
	}
	parent := parents.parent(objectNode)
	if parent == nil || parent.Kind() != "assignment_expression" ||
		!javaScriptNodeSameRange(parent.ChildByFieldName("right"), objectNode) {
		return false
	}
	return javaScriptCommonJSExportName(parent.ChildByFieldName("left"), source) == "plugin"
}

func javaScriptCommonJSAssignmentTarget(node *tree_sitter.Node, source []byte) bool {
	if node == nil {
		return false
	}
	switch strings.TrimSpace(nodeText(node, source)) {
	case "module.exports", "exports":
		return true
	}
	return javaScriptCommonJSExportName(node, source) != ""
}
