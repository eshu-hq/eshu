package parser

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptIsNextJSAppExport(path string, node *tree_sitter.Node) bool {
	if !javaScriptIsNextJSAppModule(path) {
		return false
	}
	return javaScriptIsExported(node)
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

func javaScriptIsNodeMigrationExport(path string, node *tree_sitter.Node, name string) bool {
	if !javaScriptIsNodeMigrationFile(path) {
		return false
	}
	switch strings.TrimSpace(name) {
	case "up", "down":
	default:
		return false
	}
	return javaScriptIsExported(node)
}

func javaScriptIsNodeMigrationFile(path string) bool {
	relativePath := filepath.ToSlash(path)
	return strings.Contains(relativePath, "/migrations/") || strings.HasPrefix(relativePath, "migrations/")
}

func javaScriptIsTypeScriptModuleContractExport(node *tree_sitter.Node, name string, source []byte) bool {
	switch strings.TrimSpace(name) {
	case "validate", "execute":
	default:
		return false
	}
	if !javaScriptIsExported(node) {
		return false
	}
	program := javaScriptProgramNode(node)
	if program == nil {
		return false
	}
	return javaScriptProgramHasExportedConst(program, "RULE_NAME", source)
}

func javaScriptProgramNode(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node; current != nil; current = current.Parent() {
		if current.Kind() == "program" {
			return current
		}
	}
	return nil
}

func javaScriptProgramHasExportedConst(program *tree_sitter.Node, name string, source []byte) bool {
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
		if javaScriptIsExported(node) {
			found = true
		}
	})
	return found
}
