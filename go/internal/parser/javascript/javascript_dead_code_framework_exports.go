// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptIsNextJSAppExport(path string, node *tree_sitter.Node, parents *javaScriptParentLookup) bool {
	if !javaScriptIsNextJSAppModule(path) {
		return false
	}
	return javaScriptIsExported(node, parents)
}

func javaScriptIsNextJSAppModule(path string) bool {
	switch filepath.Base(path) {
	case "page.js", "page.jsx", "page.ts", "page.tsx",
		"layout.js", "layout.jsx", "layout.ts", "layout.tsx",
		"template.js", "template.jsx", "template.ts", "template.tsx",
		"error.js", "error.jsx", "error.ts", "error.tsx",
		"loading.js", "loading.jsx", "loading.ts", "loading.tsx",
		"not-found.js", "not-found.jsx", "not-found.ts", "not-found.tsx":
		return true
	default:
		return false
	}
}

func javaScriptIsNodeMigrationExport(path string, node *tree_sitter.Node, name string, parents *javaScriptParentLookup) bool {
	if !javaScriptIsNodeMigrationFile(path) {
		return false
	}
	switch strings.TrimSpace(name) {
	case "up", "down":
	default:
		return false
	}
	return javaScriptIsExported(node, parents)
}

func javaScriptIsNodeMigrationFile(path string) bool {
	relativePath := filepath.ToSlash(path)
	return strings.Contains(relativePath, "/migrations/") || strings.HasPrefix(relativePath, "migrations/")
}

func javaScriptIsTypeScriptModuleContractExport(node *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) bool {
	switch strings.TrimSpace(name) {
	case "validate", "execute":
	default:
		return false
	}
	if !javaScriptIsExported(node, parents) {
		return false
	}
	program := javaScriptProgramNode(node, parents)
	if program == nil {
		return false
	}
	return javaScriptProgramHasExportedConst(program, "RULE_NAME", source, parents)
}

func javaScriptProgramNode(node *tree_sitter.Node, parents *javaScriptParentLookup) *tree_sitter.Node {
	for current := node; current != nil; current = parents.parent(current) {
		if current.Kind() == "program" {
			return current
		}
	}
	return nil
}

func javaScriptProgramHasExportedConst(program *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) bool {
	if program == nil || strings.TrimSpace(name) == "" {
		return false
	}
	found := false
	walkNamed(program, func(node *tree_sitter.Node) {
		if found || node.Kind() != "variable_declarator" {
			return
		}
		nameNode := node.ChildByFieldName("name")
		if strings.TrimSpace(nodeText(nameNode, source)) != name {
			return
		}
		if javaScriptIsExported(node, parents) {
			found = true
		}
	})
	return found
}
