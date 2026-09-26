// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ReExportAttributeEntries recovers re-export rows for export statements
// carrying import attributes (`export * from "./x.json" with { type: "json" }`
// and the older `assert` form). Neither vendored grammar models attributes on
// exports, so both report the statement as grammar-error recovery instead of
// an export_statement node:
//
//	typescript (v0.23.2): labeled_statement[statement_identifier "export",
//	  ERROR["* from ..."|"{ ... } from ..."], ...]
//	javascript (v0.25.0): top-level ERROR["export * from ..." |
//	  "export { ... } from ..."] holding the string and clause directly
//
// The extraction reads only the string-literal specifier, the grammar's
// export_clause/export_specifier children, and an anonymous "*" marker out of
// that ERROR node, and synthesizes full_import_name from those parts. It
// never reads node text beyond the bounded "export " prefix check, so the
// unbounded text capture that issue #7056 removed cannot return through this
// path. It returns nil for declaration exports (which parse cleanly and never
// reach these shapes) and for non-relative specifiers, matching the
// ReExportEntries contract.
func ReExportAttributeEntries(
	node *tree_sitter.Node,
	source []byte,
	lang string,
) []map[string]any {
	errNode := exportAttributeErrorNode(node, source)
	if errNode == nil {
		return nil
	}
	for _, child := range directNamedChildren(errNode) {
		if isExportAttributeConflictingKind(child.Kind()) {
			return nil
		}
	}
	var specNode *tree_sitter.Node
	var clauseNode *tree_sitter.Node
	var namespaceNode *tree_sitter.Node
	for _, child := range directNamedChildren(errNode) {
		switch child.Kind() {
		case "string":
			if specNode == nil {
				clone := child
				specNode = &clone
			}
		case "export_clause":
			if clauseNode == nil {
				clone := child
				clauseNode = &clone
			}
		case "namespace_export":
			if namespaceNode == nil {
				clone := child
				namespaceNode = &clone
			}
		}
	}
	if specNode == nil {
		return nil
	}
	moduleSource := strings.Trim(strings.TrimSpace(shared.NodeText(specNode, source)), `"'`)
	if !strings.HasPrefix(strings.TrimSpace(moduleSource), ".") {
		return nil
	}
	if clauseNode != nil {
		specifiers := exportAttributeSpecifiers(errNode, source)
		if len(specifiers) == 0 {
			return nil
		}
		fullImportName := fmt.Sprintf(
			"export { %s } from %q",
			joinExportAttributeSpecifiers(specifiers),
			moduleSource,
		)
		items := make([]map[string]any, 0, len(specifiers))
		for _, specifier := range specifiers {
			items = append(items, reExportEntry(
				specifier.ExportedName,
				specifier.OriginalName,
				moduleSource,
				fullImportName,
				specifier.lineNumber,
				lang,
			))
		}
		return items
	}
	if namespaceNode != nil || exportAttributeHasStarMarker(errNode, source) {
		return []map[string]any{reExportEntry(
			"*",
			"*",
			moduleSource,
			fmt.Sprintf("export * from %q", moduleSource),
			shared.NodeLine(specNode),
			lang,
		)}
	}
	return nil
}

// exportAttributeErrorNode returns the ERROR node holding a broken
// export-with-attributes statement, or nil when node is not one of the two
// known recovery shapes.
func exportAttributeErrorNode(node *tree_sitter.Node, source []byte) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "labeled_statement":
		if !exportAttributeLabelIsExport(node, source) {
			return nil
		}
		for _, child := range directNamedChildren(node) {
			if child.Kind() == "ERROR" {
				clone := child
				return &clone
			}
		}
		return nil
	case "ERROR":
		if !strings.HasPrefix(strings.TrimSpace(shared.NodeText(node, source)), "export ") {
			return nil
		}
		clone := *node
		return &clone
	default:
		return nil
	}
}

// exportAttributeLabelIsExport reports whether a labeled_statement is an
// "export" label, the shape the TypeScript grammar produces for an export
// statement it cannot model.
func exportAttributeLabelIsExport(node *tree_sitter.Node, source []byte) bool {
	for _, child := range directNamedChildren(node) {
		if child.Kind() == "statement_identifier" {
			return strings.TrimSpace(shared.NodeText(&child, source)) == "export"
		}
	}
	return false
}

// isExportAttributeConflictingKind reports whether a direct ERROR child proves
// the broken statement is not a re-export: a declaration export carries its
// declaration, never a bare specifier string plus clause.
func isExportAttributeConflictingKind(kind string) bool {
	switch kind {
	case "declaration",
		"class_declaration",
		"abstract_class_declaration",
		"function_declaration",
		"generator_function_declaration",
		"lexical_declaration",
		"variable_declarator",
		"variable_declaration",
		"type_alias_declaration",
		"interface_declaration",
		"enum_declaration",
		"internal_module",
		"import_alias",
		"import_statement",
		"export_statement":
		return true
	default:
		return false
	}
}

// exportAttributeHasStarMarker reports whether an ERROR node carries the
// anonymous "*" token of a star re-export. The star is not a named node, so
// the grammar's export_clause test cannot see it; reading the single marker
// token keeps invalid bare exports (`export foo from "./x"`) from collapsing
// into star rows.
func exportAttributeHasStarMarker(errNode *tree_sitter.Node, source []byte) bool {
	count := errNode.ChildCount()
	for i := range count {
		child := errNode.Child(i)
		if child == nil || child.IsNamed() {
			continue
		}
		if strings.TrimSpace(shared.NodeText(child, source)) == "*" {
			return true
		}
	}
	return false
}

// exportAttributeSpecifiers collects the grammar's export_specifier children
// under an ERROR node. Unlike ReExportSpecifiers it never falls back to
// brace-text scanning: an ERROR node's text is unbounded recovery output, not
// a statement.
func exportAttributeSpecifiers(errNode *tree_sitter.Node, source []byte) []ReExportSpecifier {
	specifiers := make([]ReExportSpecifier, 0)
	shared.WalkNamed(errNode, func(candidate *tree_sitter.Node) {
		if candidate.Kind() != "export_specifier" {
			return
		}
		nameNode := candidate.ChildByFieldName("name")
		aliasNode := candidate.ChildByFieldName("alias")
		OriginalName := strings.TrimSpace(shared.NodeText(nameNode, source))
		ExportedName := strings.TrimSpace(shared.NodeText(aliasNode, source))
		if ExportedName == "" {
			ExportedName = OriginalName
		}
		if ExportedName == "" || OriginalName == "" {
			return
		}
		specifiers = append(specifiers, ReExportSpecifier{
			ExportedName: ExportedName,
			OriginalName: OriginalName,
			lineNumber:   shared.NodeLine(candidate),
		})
	})
	return specifiers
}

// joinExportAttributeSpecifiers renders specifiers the way the source clause
// spells them, so the synthesized full_import_name stays bounded and readable.
func joinExportAttributeSpecifiers(specifiers []ReExportSpecifier) string {
	parts := make([]string, 0, len(specifiers))
	for _, specifier := range specifiers {
		if specifier.OriginalName == specifier.ExportedName {
			parts = append(parts, specifier.ExportedName)
			continue
		}
		parts = append(parts, specifier.OriginalName+" as "+specifier.ExportedName)
	}
	return strings.Join(parts, ", ")
}

// directNamedChildren returns the direct named children of node without
// descending further, so ERROR-node recovery reads only what the broken
// statement holds itself.
func directNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	out := make([]tree_sitter.Node, 0, len(children))
	for _, child := range children {
		child := child
		out = append(out, child)
	}
	return out
}
