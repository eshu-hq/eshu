// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ImportEntries returns one record per symbol an import statement binds,
// collapsing a side-effect-only import to a single record naming the module.
// node must be a non-nil import_statement; the result is nil when the statement
// carries no module specifier.
func ImportEntries(node *tree_sitter.Node, source []byte, lang string) []map[string]any {
	sourceNode := node.ChildByFieldName("source")
	moduleSource := strings.Trim(shared.NodeText(sourceNode, source), `"'`)
	if strings.TrimSpace(moduleSource) == "" {
		// TypeScript import-equals (`import x = require("./x")`) carries
		// its specifier on the import_require_clause child instead of the
		// statement's source field (issue #7059).
		return importEqualsEntries(node, source, lang)
	}

	importNode := node.ChildByFieldName("import")
	if importNode == nil {
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if child.Kind() == "string" {
				continue
			}
			importNode = &child
			break
		}
	}
	if importNode == nil {
		return []map[string]any{{
			"name":        moduleSource,
			"source":      moduleSource,
			"line_number": shared.NodeLine(sourceNode),
			"lang":        lang,
		}}
	}

	items := make([]map[string]any, 0)
	cursor := importNode.Walk()
	defer cursor.Close()
	children := importNode.NamedChildren(cursor)
	if len(children) == 0 {
		children = []tree_sitter.Node{*importNode}
	}
	for _, child := range children {
		child := child
		switch child.Kind() {
		case "import_clause":
			clauseCursor := child.Walk()
			defer clauseCursor.Close()
			for _, clauseChild := range child.NamedChildren(clauseCursor) {
				clauseChild := clauseChild
				items = append(items, importEntriesFromClause(&clauseChild, moduleSource, source, lang)...)
			}
		case "identifier":
			items = append(items, importEntriesFromClause(&child, moduleSource, source, lang)...)
		case "namespace_import", "named_imports":
			items = append(items, importEntriesFromClause(&child, moduleSource, source, lang)...)
		}
	}
	if len(items) == 0 {
		items = append(items, map[string]any{
			"name":        moduleSource,
			"source":      moduleSource,
			"line_number": shared.NodeLine(sourceNode),
			"lang":        lang,
		})
	}
	return items
}

// importEqualsEntries returns the require row for a TypeScript import-equals
// statement (`import x = require("./x")`). The grammar models the binding as
// an import_require_clause child whose source field holds the specifier, so
// this runs only when the statement itself has no source field. It returns
// nil for the entity-name form (`import x = A.B`), which names a value rather
// than a module and carries no string specifier.
func importEqualsEntries(
	node *tree_sitter.Node,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil || node.Kind() != "import_statement" {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "import_require_clause" {
			continue
		}
		if item := importRequireClauseEntry(&child, source, lang); item != nil {
			return []map[string]any{item}
		}
	}
	return nil
}

// importRequireClauseEntry renders one import_require_clause
// (`x = require("./x")`) as a require import row, mirroring the
// `const x = require("./x")` row shape so downstream source-based consumers
// treat both spellings of the same call identically.
func importRequireClauseEntry(
	clause *tree_sitter.Node,
	source []byte,
	lang string,
) map[string]any {
	sourceNode := clause.ChildByFieldName("source")
	if sourceNode == nil || sourceNode.Kind() != "string" {
		return nil
	}
	moduleSource := strings.Trim(strings.TrimSpace(shared.NodeText(sourceNode, source)), `"'`)
	if moduleSource == "" {
		return nil
	}
	var alias string
	clauseCursor := clause.Walk()
	defer clauseCursor.Close()
	for _, child := range clause.NamedChildren(clauseCursor) {
		child := child
		if child.Kind() == "identifier" {
			alias = strings.TrimSpace(shared.NodeText(&child, source))
			break
		}
	}
	if alias == "" {
		return nil
	}
	return map[string]any{
		"name":             "*",
		"alias":            alias,
		"source":           moduleSource,
		"import_type":      "require",
		"full_import_name": fmt.Sprintf("import %s = require(%q)", alias, moduleSource),
		"line_number":      shared.NodeLine(clause),
		"lang":             lang,
	}
}

func importEntriesFromClause(
	node *tree_sitter.Node,
	moduleSource string,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil {
		return nil
	}

	switch node.Kind() {
	case "identifier":
		return []map[string]any{{
			"name":        "default",
			"source":      moduleSource,
			"alias":       shared.NodeText(node, source),
			"line_number": shared.NodeLine(node),
			"lang":        lang,
		}}
	case "namespace_import":
		alias := NamespaceImportAlias(node, source)
		return []map[string]any{{
			"name":        "*",
			"source":      moduleSource,
			"alias":       alias,
			"line_number": shared.NodeLine(node),
			"lang":        lang,
		}}
	case "named_imports":
		items := make([]map[string]any, 0)
		cursor := node.Walk()
		defer cursor.Close()
		for _, specifier := range node.NamedChildren(cursor) {
			specifier := specifier
			if specifier.Kind() != "import_specifier" {
				continue
			}
			nameNode := specifier.ChildByFieldName("name")
			aliasNode := specifier.ChildByFieldName("alias")
			items = append(items, map[string]any{
				"name":        shared.NodeText(nameNode, source),
				"source":      moduleSource,
				"alias":       shared.NodeText(aliasNode, source),
				"line_number": shared.NodeLine(&specifier),
				"lang":        lang,
			})
		}
		return items
	default:
		return nil
	}
}

// NamespaceImportAlias returns the local binding of a "* as name" namespace
// import, reading the grammar's name field and falling back to the statement
// text. It returns an empty string for any other import form.
func NamespaceImportAlias(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if aliasNode := node.ChildByFieldName("name"); aliasNode != nil {
		if alias := strings.TrimSpace(shared.NodeText(aliasNode, source)); alias != "" {
			return alias
		}
	}
	text := strings.TrimSpace(shared.NodeText(node, source))
	parts := strings.Fields(text)
	if len(parts) >= 3 && parts[0] == "*" && parts[1] == "as" {
		return parts[2]
	}
	return ""
}
