// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func pythonAioHTTPWebSymbols(root *tree_sitter.Node, source []byte) map[string]struct{} {
	symbols := make(map[string]struct{})
	pythonWalkImportStatements(root, source, func(statement string) {
		switch {
		case strings.HasPrefix(statement, "from aiohttp import "):
			importClause := strings.TrimSpace(strings.TrimPrefix(statement, "from aiohttp import "))
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(importClause, "("), ")"))) {
				name, alias := pythonSplitImportAlias(clause)
				if name != "web" {
					continue
				}
				if alias == "" {
					alias = name
				}
				symbols[alias] = struct{}{}
			}
		case strings.HasPrefix(statement, "import "):
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimPrefix(statement, "import "))) {
				modulePath, alias := pythonSplitImportAlias(clause)
				if modulePath != "aiohttp.web" {
					continue
				}
				if alias == "" {
					symbols[modulePath] = struct{}{}
					continue
				}
				symbols[alias] = struct{}{}
			}
		}
	})
	return symbols
}

func pythonTornadoImportSymbols(root *tree_sitter.Node, source []byte) pythonTornadoImports {
	imports := pythonTornadoImports{
		moduleObjects:           make(map[string]struct{}),
		applicationConstructors: make(map[string]struct{}),
		urlSpecConstructors:     make(map[string]struct{}),
	}
	pythonWalkImportStatements(root, source, func(statement string) {
		switch {
		case strings.HasPrefix(statement, "import "):
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimPrefix(statement, "import "))) {
				modulePath, alias := pythonSplitImportAlias(clause)
				if modulePath != "tornado.web" {
					continue
				}
				if alias == "" {
					imports.moduleObjects[modulePath] = struct{}{}
					continue
				}
				imports.moduleObjects[alias] = struct{}{}
			}
		case strings.HasPrefix(statement, "from tornado.web import "):
			importClause := strings.TrimSpace(strings.TrimPrefix(statement, "from tornado.web import "))
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(importClause, "("), ")"))) {
				name, alias := pythonSplitImportAlias(clause)
				if alias == "" {
					alias = name
				}
				switch name {
				case "Application":
					imports.applicationConstructors[alias] = struct{}{}
				case "URLSpec", "url":
					imports.urlSpecConstructors[alias] = struct{}{}
				}
			}
		}
	})
	return imports
}

func pythonWalkImportStatements(root *tree_sitter.Node, source []byte, visit func(statement string)) {
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "import_statement" && node.Kind() != "import_from_statement" {
			return
		}
		statement := strings.Join(strings.Fields(strings.TrimSpace(nodeText(node, source))), " ")
		if statement != "" {
			visit(statement)
		}
	})
}

func pythonCallTargetsAnyObjectAttribute(
	function *tree_sitter.Node,
	source []byte,
	objects map[string]struct{},
	attribute string,
) bool {
	if function == nil || function.Kind() != "attribute" {
		return false
	}
	if strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source)) != attribute {
		return false
	}
	return pythonNodeTextInSet(function.ChildByFieldName("object"), source, objects)
}

func pythonNodeTextInSet(node *tree_sitter.Node, source []byte, values map[string]struct{}) bool {
	if node == nil || len(values) == 0 {
		return false
	}
	_, ok := values[strings.TrimSpace(nodeText(node, source))]
	return ok
}

// pythonWalkImportStatementsGathered mirrors pythonWalkImportStatements
// but iterates a pre-gathered slice of import_statement / import_from_statement
// nodes instead of walking the full tree.
func pythonWalkImportStatementsGathered(gathered []*tree_sitter.Node, source []byte, visit func(statement string)) {
	for _, node := range gathered {
		statement := strings.Join(strings.Fields(nodeText(node, source)), " ")
		if statement != "" {
			visit(statement)
		}
	}
}
