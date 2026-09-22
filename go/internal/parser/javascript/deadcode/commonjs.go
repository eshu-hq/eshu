// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func MergeRegisteredRootKinds(dst map[string][]string, src map[string][]string) {
	for name, rootKinds := range src {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		for _, rootKind := range rootKinds {
			dst[key] = shared.AppendUniqueString(dst[key], rootKind)
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
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		leftNode := node.ChildByFieldName("left")
		if CommonJSExportName(leftNode, source) == "" {
			return
		}
		rightNode := node.ChildByFieldName("right")
		exportedName := javaScriptCommonJSAliasTargetName(rightNode, source)
		if exportedName == "" {
			return
		}
		key := strings.ToLower(exportedName)
		registered[key] = shared.AppendUniqueString(registered[key], rootKind)
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
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		leftNode := node.ChildByFieldName("left")
		if strings.TrimSpace(shared.NodeText(leftNode, source)) != "module.exports" {
			return
		}
		rightNode := node.ChildByFieldName("right")
		exportedName := syntax.IdentifierName(rightNode, source)
		if exportedName == "" {
			return
		}
		key := strings.ToLower(exportedName)
		registered[key] = shared.AppendUniqueString(registered[key], "javascript.commonjs_default_export")
	})
	return registered
}

// CollectCommonJSModuleExportAlias records dst[name] = struct{}{}
// for one variable_declarator node that aliases module.exports to a local
// name (`const alias = module.exports`). It is a no-op for any other node
// kind, so callers may invoke it on every visited node in a shared traversal
// without pre-filtering.
func CollectCommonJSModuleExportAlias(node *tree_sitter.Node, source []byte, dst map[string]struct{}) {
	if node.Kind() != "variable_declarator" {
		return
	}
	valueNode := node.ChildByFieldName("value")
	if strings.TrimSpace(shared.NodeText(valueNode, source)) != "module.exports" {
		return
	}
	name := syntax.IdentifierName(node.ChildByFieldName("name"), source)
	if name == "" {
		return
	}
	dst[name] = struct{}{}
}

func javaScriptMethodInsideCommonJSDefaultExport(node *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) bool {
	if node == nil || node.Kind() != "method_definition" {
		return false
	}
	classNode := javaScriptNearestClassNode(node, parents)
	if classNode == nil {
		return false
	}
	for current := parents.Parent(classNode); current != nil; current = parents.Parent(current) {
		if current.Kind() == "program" {
			return false
		}
		if current.Kind() != "assignment_expression" {
			continue
		}
		leftNode := current.ChildByFieldName("left")
		rightNode := current.ChildByFieldName("right")
		if strings.TrimSpace(shared.NodeText(leftNode, source)) == "module.exports" &&
			javaScriptAssignmentRightChainContains(rightNode, classNode) {
			return true
		}
	}
	return false
}

func javaScriptNearestClassNode(node *tree_sitter.Node, parents *syntax.ParentLookup) *tree_sitter.Node {
	for current := parents.Parent(node); current != nil; current = parents.Parent(current) {
		switch current.Kind() {
		case "class", "class_declaration", "abstract_class_declaration":
			return current
		case "program":
			return nil
		}
	}
	return nil
}

func javaScriptAssignmentRightChainContains(valueNode *tree_sitter.Node, target *tree_sitter.Node) bool {
	if syntax.NodeSameRange(valueNode, target) {
		return true
	}
	if valueNode == nil || valueNode.Kind() != "assignment_expression" {
		return false
	}
	return javaScriptAssignmentRightChainContains(valueNode.ChildByFieldName("right"), target)
}

func RewriteCommonJSModuleExportAliasFullName(fullName string, aliases map[string]struct{}) string {
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

func javaScriptIsCommonJSExport(node *tree_sitter.Node, name string, source []byte, parents *syntax.ParentLookup) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for current := node; current != nil; current = parents.Parent(current) {
		if current.Kind() != "assignment_expression" {
			continue
		}
		leftNode := current.ChildByFieldName("left")
		exportName := CommonJSExportName(leftNode, source)
		return exportName == name
	}
	return false
}

func javaScriptIsCommonJSMixinExport(node *tree_sitter.Node, name string, source []byte, parents *syntax.ParentLookup) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for current := node; current != nil; current = parents.Parent(current) {
		if current.Kind() != "assignment_expression" {
			continue
		}
		leftNode := current.ChildByFieldName("left")
		if leftNode == nil || leftNode.Kind() != "member_expression" {
			return false
		}
		objectNode := leftNode.ChildByFieldName("object")
		objectText := strings.TrimSpace(shared.NodeText(objectNode, source))
		return objectText == "module.exports.mixin" || objectText == "exports.mixin"
	}
	return false
}

func CommonJSExportName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	objectNode, propertyNode := javaScriptCommonJSExportTargetNodes(node)
	if objectNode == nil || propertyNode == nil {
		return ""
	}
	objectText := strings.TrimSpace(shared.NodeText(objectNode, source))
	switch {
	case objectText == "module.exports" || strings.HasPrefix(objectText, "module.exports."):
		return syntax.FunctionName(propertyNode, source)
	case objectText == "exports" || strings.HasPrefix(objectText, "exports."):
		return syntax.FunctionName(propertyNode, source)
	default:
		return ""
	}
}

func javaScriptCommonJSExportTargetNodes(node *tree_sitter.Node) (*tree_sitter.Node, *tree_sitter.Node) {
	switch node.Kind() {
	case "member_expression":
		return node.ChildByFieldName("object"), node.ChildByFieldName("property")
	case "subscript_expression":
		return node.ChildByFieldName("object"), node.ChildByFieldName("index")
	default:
		return nil, nil
	}
}

func javaScriptCommonJSAliasTargetName(node *tree_sitter.Node, source []byte) string {
	if name := syntax.IdentifierName(node, source); name != "" {
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
	if strings.TrimSpace(shared.NodeText(objectNode, source)) != "module.exports" {
		return ""
	}
	return syntax.FunctionName(propertyNode, source)
}

func ExportAssignmentNameNode(node *tree_sitter.Node, source []byte) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if CommonJSExportName(node, source) == "" {
		return nil
	}
	_, propertyNode := javaScriptCommonJSExportTargetNodes(node)
	return shared.CloneNode(propertyNode)
}
