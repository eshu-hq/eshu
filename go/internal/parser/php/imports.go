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

	importType := phpImportKind(node)
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
		if importType == "use" && !phpUseClauseHasKindKeyword(clause) {
			state.packageKeys.recordUse(name, alias)
		}
		if importType == "function" || (importType == "use" && phpUseClauseIsFunction(clause)) {
			state.packageKeys.recordFunctionUse(name, alias)
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

// phpUseClauseHasKindKeyword reports whether one use clause carries its own
// `function` or `const` keyword, as in the mixed group
// `use A\{B, function c, const D}`. The statement-level phpImportKind reads
// such a declaration as a class import, so the package-key binding checks the
// clause itself: a function or constant clause binds no class name.
func phpUseClauseHasKindKeyword(clause *tree_sitter.Node) bool {
	for index := uint(0); index < clause.ChildCount(); index++ {
		child := clause.Child(index)
		if child == nil || child.IsNamed() {
			continue
		}
		switch child.Kind() {
		case "function", "const":
			return true
		}
	}
	return false
}

// phpImportKind returns "function", "const", or "use" for a use declaration.
// It reads the grammar's `type` field, the `function` or `const` kind token,
// never the statement text: a namespace such as `Functional\Collection` or
// `Constraint\Rule` only looks like a kind keyword as a text prefix (#8222).
// A grouped declaration (`use function A\{b, c}`) carries the token on the
// declaration; an ungrouped one carries it on its first clause, which PHP
// applies to every clause of `use function a\b, c\d;`. A mixed group such as
// `use A\{B, function c}` has no declaration-level token and stays "use".
func phpImportKind(node *tree_sitter.Node) string {
	kindNode := node.ChildByFieldName("type")
	if kindNode == nil {
		kindNode = phpFirstUseClauseKindNode(node)
	}
	if kindNode == nil {
		return "use"
	}
	switch kindNode.Kind() {
	case "function":
		return "function"
	case "const":
		return "const"
	}
	return "use"
}

// phpFirstUseClauseKindNode returns the kind token of the first direct
// namespace_use_clause of an ungrouped use declaration, or nil when it has none
// or the declaration is grouped (a group's kind lives on the declaration).
func phpFirstUseClauseKindNode(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "namespace_use_group":
			return nil
		case "namespace_use_clause":
			return child.ChildByFieldName("type")
		}
	}
	return nil
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
