package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func mergeJavaScriptRegisteredRootKinds(dst map[string][]string, src map[string][]string) {
	for name, rootKinds := range src {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		for _, rootKind := range rootKinds {
			dst[key] = appendUniqueString(dst[key], rootKind)
		}
	}
}

func javaScriptCommonJSExportAliasRootKinds(
	root *tree_sitter.Node,
	source []byte,
	rootKind string,
) map[string][]string {
	registered := make(map[string][]string)
	if root == nil || strings.TrimSpace(rootKind) == "" {
		return registered
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		leftNode := node.ChildByFieldName("left")
		if javaScriptCommonJSExportName(leftNode, source) == "" {
			return
		}
		rightNode := node.ChildByFieldName("right")
		exportedName := javaScriptCommonJSAliasTargetName(rightNode, source)
		if exportedName == "" {
			return
		}
		key := strings.ToLower(exportedName)
		registered[key] = appendUniqueString(registered[key], rootKind)
	})
	return registered
}

func javaScriptCommonJSDefaultExportAliasRootKinds(
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
		if strings.TrimSpace(nodeText(leftNode, source)) != "module.exports" {
			return
		}
		rightNode := node.ChildByFieldName("right")
		exportedName := javaScriptIdentifierName(rightNode, source)
		if exportedName == "" {
			return
		}
		key := strings.ToLower(exportedName)
		registered[key] = appendUniqueString(registered[key], "javascript.commonjs_default_export")
	})
	return registered
}

func javaScriptCommonJSModuleExportAliases(root *tree_sitter.Node, source []byte) map[string]struct{} {
	aliases := make(map[string]struct{})
	if root == nil {
		return aliases
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		valueNode := node.ChildByFieldName("value")
		if strings.TrimSpace(nodeText(valueNode, source)) != "module.exports" {
			return
		}
		name := javaScriptIdentifierName(node.ChildByFieldName("name"), source)
		if name == "" {
			return
		}
		aliases[name] = struct{}{}
	})
	return aliases
}

func rewriteJavaScriptCommonJSModuleExportAliasFullName(fullName string, aliases map[string]struct{}) string {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" || len(aliases) == 0 {
		return fullName
	}
	for alias := range aliases {
		prefix := alias + "."
		if strings.HasPrefix(fullName, prefix) {
			return "module.exports." + strings.TrimPrefix(fullName, prefix)
		}
	}
	return fullName
}

func javaScriptIsCommonJSExport(node *tree_sitter.Node, name string, source []byte) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for current := node; current != nil; current = current.Parent() {
		if current.Kind() != "assignment_expression" {
			continue
		}
		leftNode := current.ChildByFieldName("left")
		exportName := javaScriptCommonJSExportName(leftNode, source)
		return exportName == name
	}
	return false
}

func javaScriptIsCommonJSMixinExport(node *tree_sitter.Node, name string, source []byte) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for current := node; current != nil; current = current.Parent() {
		if current.Kind() != "assignment_expression" {
			continue
		}
		leftNode := current.ChildByFieldName("left")
		if leftNode == nil || leftNode.Kind() != "member_expression" {
			return false
		}
		objectNode := leftNode.ChildByFieldName("object")
		objectText := strings.TrimSpace(nodeText(objectNode, source))
		return objectText == "module.exports.mixin" || objectText == "exports.mixin"
	}
	return false
}

func javaScriptCommonJSExportName(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "member_expression" {
		return ""
	}
	objectNode := node.ChildByFieldName("object")
	propertyNode := node.ChildByFieldName("property")
	if objectNode == nil || propertyNode == nil {
		return ""
	}
	objectText := strings.TrimSpace(nodeText(objectNode, source))
	switch {
	case objectText == "module.exports" || strings.HasPrefix(objectText, "module.exports."):
		return javaScriptIdentifierName(propertyNode, source)
	case objectText == "exports" || strings.HasPrefix(objectText, "exports."):
		return javaScriptIdentifierName(propertyNode, source)
	default:
		return ""
	}
}

func javaScriptCommonJSAliasTargetName(node *tree_sitter.Node, source []byte) string {
	if name := javaScriptIdentifierName(node, source); name != "" {
		return name
	}
	if node == nil || node.Kind() != "member_expression" {
		return ""
	}
	objectNode := node.ChildByFieldName("object")
	propertyNode := node.ChildByFieldName("property")
	if objectNode == nil || propertyNode == nil {
		return ""
	}
	if strings.TrimSpace(nodeText(objectNode, source)) != "module.exports" {
		return ""
	}
	return javaScriptIdentifierName(propertyNode, source)
}

func javaScriptExportAssignmentNameNode(node *tree_sitter.Node, source []byte) *tree_sitter.Node {
	if node == nil || node.Kind() != "member_expression" {
		return nil
	}
	if javaScriptCommonJSExportName(node, source) == "" {
		return nil
	}
	propertyNode := node.ChildByFieldName("property")
	return cloneNode(propertyNode)
}
