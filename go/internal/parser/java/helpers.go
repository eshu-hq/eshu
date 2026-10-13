// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func readSource(path string) ([]byte, error) {
	return shared.ReadSource(path)
}

func walkNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	shared.WalkNamed(node, visit)
}

func walkDirectNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if node == nil {
		return
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		visit(&child)
	}
}

func nodeText(node *tree_sitter.Node, source []byte) string {
	return shared.NodeText(node, source)
}

func nodeLine(node *tree_sitter.Node) int {
	return shared.NodeLine(node)
}

func nodeEndLine(node *tree_sitter.Node) int {
	return shared.NodeEndLine(node)
}

func cloneNode(node *tree_sitter.Node) *tree_sitter.Node {
	return shared.CloneNode(node)
}

func basePayload(path string, lang string, isDependency bool) map[string]any {
	return shared.BasePayload(path, lang, isDependency)
}

func appendBucket(payload map[string]any, key string, item map[string]any) {
	shared.AppendBucket(payload, key, item)
}

func sortNamedBucket(payload map[string]any, key string) {
	shared.SortNamedBucket(payload, key)
}

func appendNamedType(payload map[string]any, bucket string, node *tree_sitter.Node, source []byte, lang string) {
	nameNode := node.ChildByFieldName("name")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	appendBucket(payload, bucket, map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        lang,
	})
}

func nearestNamedAncestor(node *tree_sitter.Node, source []byte, kinds ...string) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		for _, kind := range kinds {
			if current.Kind() != kind {
				continue
			}
			return nodeText(current.ChildByFieldName("name"), source)
		}
	}
	return ""
}

func firstNamedDescendant(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	var found *tree_sitter.Node
	walkNamed(node, func(child *tree_sitter.Node) {
		if found != nil {
			return
		}
		for _, kind := range kinds {
			if child.Kind() == kind {
				found = cloneNode(child)
				return
			}
		}
	})
	return found
}

// javaFilePackageName reads the file's own declared package for package-level
// import cycles (#7349): the single file-scoped package_declaration. A file
// with no declaration lives in the default package and reports "", never a
// guess: an invented package would collapse the wrong files together.
func javaFilePackageName(root *tree_sitter.Node, source []byte) string {
	packageName := ""
	walkNamed(root, func(node *tree_sitter.Node) {
		if packageName != "" || node.Kind() != "package_declaration" {
			return
		}
		walkDirectNamed(node, func(child *tree_sitter.Node) {
			if packageName == "" {
				packageName = strings.TrimSpace(nodeText(child, source))
			}
		})
		if packageName == "" {
			raw := strings.TrimSpace(nodeText(node, source))
			raw = strings.TrimPrefix(raw, "package")
			raw = strings.TrimSuffix(raw, ";")
			packageName = strings.TrimSpace(raw)
		}
	})
	return packageName
}

func javaImportName(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		return nodeText(&child, source)
	}
	return ""
}

func appendJavaImport(payload map[string]any, node *tree_sitter.Node, source []byte) {
	name := strings.TrimSpace(javaImportName(node, source))
	if name == "" {
		return
	}

	raw := strings.TrimSpace(nodeText(node, source))
	importPath := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "import"), ";"))
	importType := "import"
	if strings.HasPrefix(importPath, "static ") {
		importType = "static"
		importPath = strings.TrimSpace(strings.TrimPrefix(importPath, "static "))
	}

	appendBucket(payload, "imports", map[string]any{
		"name":             name,
		"source":           importPath,
		"alias":            javaImportAlias(importPath),
		"full_import_name": raw,
		"import_type":      importType,
		"line_number":      nodeLine(node),
		"lang":             "java",
	})
}

func javaImportAlias(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "."); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+1:])
	}
	return trimmed
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
