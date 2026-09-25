// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// exportImportEqualsEntries returns the require row for a TypeScript
// `export import X = require("./x")` statement (issue #7059). The grammar
// models the alias as the export_statement's declaration but leaves the
// require's string argument in the following sibling expression statement, so
// the entry joins the two only when that sibling starts on the same line.
// Statements the grammar models natively (a source field is present) stay
// owned by ReExportEntries.
func exportImportEqualsEntries(
	node *tree_sitter.Node,
	parents *syntax.ParentLookup,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil || node.Kind() != "export_statement" {
		return nil
	}
	if syntax.ReExportSource(node, source) != "" {
		return nil
	}
	decl := node.ChildByFieldName("declaration")
	if decl == nil || decl.Kind() != "import_alias" {
		return nil
	}
	alias := exportImportEqualsAlias(decl, source)
	if alias == "" {
		return nil
	}
	moduleSource := exportImportEqualsModuleSource(node, parents, source)
	if moduleSource == "" {
		return nil
	}
	return []map[string]any{{
		"name":             "*",
		"alias":            alias,
		"source":           moduleSource,
		"import_type":      "require",
		"full_import_name": fmt.Sprintf("export import %s = require(%q)", alias, moduleSource),
		"line_number":      nodeLine(node),
		"lang":             lang,
	}}
}

// exportImportEqualsAlias returns the local binding of an `import X = require`
// alias when its target is a require call, or an empty string for the
// entity-name form (`import X = A.B`) and any other shape.
func exportImportEqualsAlias(decl *tree_sitter.Node, source []byte) string {
	cursor := decl.Walk()
	children := decl.NamedChildren(cursor)
	cursor.Close()
	names := make([]string, 0, len(children))
	for i := range children {
		if children[i].Kind() != "identifier" {
			return ""
		}
		names = append(names, strings.TrimSpace(shared.NodeText(&children[i], source)))
	}
	if len(names) != 2 || names[0] == "" || names[1] != "require" {
		return ""
	}
	return names[0]
}

// exportImportEqualsModuleSource returns the require's string argument from
// the expression statement immediately following an `export import` statement,
// or an empty string unless that sibling is a same-line parenthesized single
// string.
func exportImportEqualsModuleSource(
	node *tree_sitter.Node,
	parents *syntax.ParentLookup,
	source []byte,
) string {
	parent := parents.Parent(node)
	if parent == nil {
		return ""
	}
	count := parent.ChildCount()
	sawNode := false
	for i := range count {
		child := parent.Child(i)
		if child == nil {
			continue
		}
		if !sawNode {
			if child.Id() == node.Id() {
				sawNode = true
			}
			continue
		}
		if !child.IsNamed() {
			continue
		}
		if child.Kind() == "empty_statement" {
			continue
		}
		if child.Kind() != "expression_statement" {
			return ""
		}
		if child.StartPosition().Row != node.StartPosition().Row {
			return ""
		}
		return parenthesizedStringValue(child, source)
	}
	return ""
}

// parenthesizedStringValue returns the unquoted string of a `("...")`
// expression statement, or an empty string for any other shape.
func parenthesizedStringValue(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		if children[i].Kind() != "parenthesized_expression" {
			continue
		}
		inner := children[i].Walk()
		innerChildren := children[i].NamedChildren(inner)
		inner.Close()
		if len(innerChildren) != 1 || innerChildren[0].Kind() != "string" {
			return ""
		}
		return strings.Trim(strings.TrimSpace(shared.NodeText(&innerChildren[0], source)), `"'`)
	}
	return ""
}
