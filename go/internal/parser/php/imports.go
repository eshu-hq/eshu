// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectPHPImports emits import rows for a `use` declaration, handling single
// clauses, grouped clauses, and the function/const import kinds. Aliases are
// recorded so receiver inference can resolve use-imported short names.
func collectPHPImports(state *phpParseState, node *tree_sitter.Node) {
	if phpNodeInsideType(node) {
		return
	}

	importType := phpImportKind(node, state.source)
	fullImportName := phpUseStatementText(node, state.source)
	lineNumber := shared.NodeLine(node)
	prefix := phpUseGroupPrefix(node, state.source)

	for _, clause := range phpUseClauses(node) {
		name, alias := phpUseClauseNameAndAlias(clause, state.source, prefix)
		if name == "" {
			continue
		}
		if importType == "use" && alias != "" {
			state.importAliases[alias] = normalizePHPTypeName(name)
		}
		shared.AppendBucket(state.payload, "imports", map[string]any{
			"name":             name,
			"full_import_name": fullImportName,
			"line_number":      lineNumber,
			"alias":            alias,
			"import_type":      importType,
			"context":          []any{nil, nil},
			"lang":             "php",
			"is_dependency":    false,
		})
	}
}

// phpUseClauses returns every namespace_use_clause under a use declaration,
// flattening grouped clauses under namespace_use_group.
func phpUseClauses(node *tree_sitter.Node) []*tree_sitter.Node {
	var clauses []*tree_sitter.Node
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "namespace_use_clause":
			clauses = append(clauses, shared.CloneNode(&child))
		case "namespace_use_group":
			groupCursor := child.Walk()
			for _, groupChild := range child.NamedChildren(groupCursor) {
				groupChild := groupChild
				if groupChild.Kind() == "namespace_use_clause" {
					clauses = append(clauses, shared.CloneNode(&groupChild))
				}
			}
			groupCursor.Close()
		}
	}
	return clauses
}

// phpUseClauseNameAndAlias returns the fully qualified import name and optional
// alias for one use clause. prefix is the group namespace, empty for single
// clauses.
func phpUseClauseNameAndAlias(clause *tree_sitter.Node, source []byte, prefix string) (string, string) {
	var nameParts []string
	alias := ""
	nameNode := phpUseClauseNameNode(clause)
	if nameNode != nil {
		nameParts = append(nameParts, strings.TrimSpace(shared.NodeText(nameNode, source)))
	}
	aliasNode := phpUseClauseAliasNode(clause, nameNode)
	if aliasNode != nil {
		alias = strings.TrimSpace(shared.NodeText(aliasNode, source))
	}
	name := strings.Join(nameParts, "")
	if prefix != "" && name != "" {
		name = prefix + `\` + name
	}
	return strings.TrimSpace(strings.Trim(name, `\`)), alias
}

// phpUseClauseNameNode returns the qualified_name or first name node naming the
// imported symbol.
func phpUseClauseNameNode(clause *tree_sitter.Node) *tree_sitter.Node {
	cursor := clause.Walk()
	defer cursor.Close()
	for _, child := range clause.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "qualified_name", "name":
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// phpUseClauseAliasNode returns the alias name node when a clause renames the
// imported symbol, i.e. the trailing bare name that is not the symbol name.
func phpUseClauseAliasNode(clause *tree_sitter.Node, nameNode *tree_sitter.Node) *tree_sitter.Node {
	cursor := clause.Walk()
	defer cursor.Close()
	var last *tree_sitter.Node
	count := 0
	for _, child := range clause.NamedChildren(cursor) {
		child := child
		if child.Kind() == "name" {
			count++
			last = shared.CloneNode(&child)
		}
	}
	// A single bare name with no qualified_name is the symbol itself, not an alias.
	if nameNode != nil && nameNode.Kind() == "name" && count == 1 {
		return nil
	}
	if nameNode != nil && nameNode.Kind() == "qualified_name" && count >= 1 {
		return last
	}
	if nameNode != nil && nameNode.Kind() == "name" && count >= 2 {
		return last
	}
	return nil
}

// phpImportKind returns "function", "const", or "use" for a use declaration.
func phpImportKind(node *tree_sitter.Node, source []byte) string {
	text := strings.ToLower(strings.TrimSpace(shared.NodeText(node, source)))
	switch {
	case strings.HasPrefix(text, "use function"):
		return "function"
	case strings.HasPrefix(text, "use const"):
		return "const"
	default:
		return "use"
	}
}

// phpUseGroupPrefix returns the namespace prefix shared by a grouped use
// declaration, or the empty string for ungrouped declarations.
func phpUseGroupPrefix(node *tree_sitter.Node, source []byte) string {
	hasGroup := false
	prefix := ""
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "namespace_name":
			prefix = strings.Trim(strings.TrimSpace(shared.NodeText(&child, source)), `\`)
		case "namespace_use_group":
			hasGroup = true
		}
	}
	if !hasGroup {
		return ""
	}
	return prefix
}

// phpUseStatementText returns the full source text of a use statement including
// the trailing semicolon, matching the legacy full_import_name contract.
func phpUseStatementText(node *tree_sitter.Node, source []byte) string {
	text := strings.TrimSpace(shared.NodeText(node, source))
	if !strings.HasSuffix(text, ";") {
		text += ";"
	}
	return text
}

// phpNodeInsideType reports whether a node sits inside a class, interface, or
// trait declaration list, where `use` imports traits rather than namespaces.
func phpNodeInsideType(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "trait_declaration", "anonymous_class", "declaration_list":
			return true
		}
	}
	return false
}
