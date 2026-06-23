package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptTypeAliasItem(
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	source []byte,
	lang string,
	deadCodeRoots javaScriptDeadCodeEvidence,
) map[string]any {
	name := nodeText(nameNode, source)
	item := map[string]any{
		"name":            name,
		"line_number":     nodeLine(nameNode),
		"end_line":        nodeEndLine(node),
		"lang":            lang,
		"type_parameters": javaScriptTypeParameters(node, source),
	}
	if aliasKind := javaScriptTypeAliasKind(node); aliasKind != "" {
		item["type_alias_kind"] = aliasKind
	}
	if rootKinds := javaScriptDeadCodeRootKinds("", node, name, source, deadCodeRoots); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	return item
}

func javaScriptFunctionSemantics(node *tree_sitter.Node, source []byte, lang string, parents *javaScriptParentLookup) map[string]any {
	semantics := make(map[string]any)
	if classContext := javaScriptEnclosingClassName(node, source, parents); classContext != "" {
		semantics["class_context"] = classContext
	}
	if objectContext := javaScriptEnclosingObjectLiteralName(node, source, parents); objectContext != "" {
		semantics["context"] = objectContext
		semantics["context_type"] = "module"
	}
	if enclosingFunction := javaScriptEnclosingFunctionName(node, source, parents); enclosingFunction != "" {
		semantics["enclosing_function"] = enclosingFunction
	}
	if lang == "tsx" && javaScriptContainsJSXFragmentShorthand(node) {
		semantics["jsx_fragment_shorthand"] = true
	}
	if len(semantics) == 0 {
		return nil
	}
	return semantics
}

func javaScriptEnclosingFunctionName(node *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) string {
	original := node
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "function_declaration", "generator_function_declaration", "method_definition", "variable_declarator":
			if current.Kind() == "variable_declarator" && javaScriptNodeSameRange(current.ChildByFieldName("value"), original) {
				continue
			}
			name := strings.TrimSpace(javaScriptFunctionName(current.ChildByFieldName("name"), source))
			if name != "" {
				return name
			}
		case "program":
			return ""
		}
	}
	return ""
}

func javaScriptNodeSameRange(left *tree_sitter.Node, right *tree_sitter.Node) bool {
	return left != nil && right != nil && left.StartByte() == right.StartByte() && left.EndByte() == right.EndByte()
}

func javaScriptEnclosingClassName(node *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) string {
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "class_declaration", "abstract_class_declaration":
			nameNode := current.ChildByFieldName("name")
			return strings.TrimSpace(nodeText(nameNode, source))
		}
	}
	return ""
}

func javaScriptEnclosingObjectLiteralName(node *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) string {
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		if current.Kind() != "object" {
			continue
		}
		return javaScriptObjectLiteralBindingName(current, source, parents)
	}
	return ""
}

func javaScriptObjectLiteralBindingName(objectNode *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) string {
	if objectNode == nil {
		return ""
	}
	parent := parents.parent(objectNode)
	if parent == nil {
		return ""
	}
	switch parent.Kind() {
	case "variable_declarator":
		return strings.TrimSpace(nodeText(parent.ChildByFieldName("name"), source))
	case "assignment_expression":
		leftNode := parent.ChildByFieldName("left")
		if exportName := javaScriptCommonJSExportName(leftNode, source); exportName != "" {
			return exportName
		}
		return strings.TrimSpace(nodeText(leftNode, source))
	}
	return ""
}

func javaScriptTypeAliasKind(node *tree_sitter.Node) string {
	if node == nil {
		return ""
	}
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		return ""
	}
	if javaScriptNodeContainsKind(valueNode, "conditional_type") {
		return "conditional_type"
	}
	if javaScriptNodeContainsKind(valueNode, "mapped_type_clause") {
		return "mapped_type"
	}
	return ""
}

func javaScriptNamespaceModuleItem(
	node *tree_sitter.Node,
	source []byte,
	lang string,
) map[string]any {
	if node == nil {
		return nil
	}
	nameNode := node.ChildByFieldName("name")
	name := strings.TrimSpace(nodeText(nameNode, source))
	if name == "" {
		return nil
	}
	return map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        lang,
		"module_kind": "namespace",
	}
}

func javaScriptNodeContainsKind(node *tree_sitter.Node, kind string) bool {
	if node == nil {
		return false
	}
	if node.Kind() == kind {
		return true
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		if javaScriptNodeContainsKind(&child, kind) {
			return true
		}
	}
	return false
}
