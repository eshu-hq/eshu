// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ReExportEntries returns one import record per symbol a barrel re-exports from a
// relative module, collapsing "export * from" to a single "*" entry. It returns nil
// for a node that is not an export_statement, and for one whose specifier is a
// package rather than a relative path, since only relative re-exports resolve to a
// file inside the repository.
func ReExportEntries(
	node *tree_sitter.Node,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil || node.Kind() != "export_statement" {
		return nil
	}

	sourceNode := node.ChildByFieldName("source")
	moduleSource := ReExportSource(node, source)
	if !strings.HasPrefix(strings.TrimSpace(moduleSource), ".") {
		return nil
	}

	fullImportName := strings.TrimSpace(shared.NodeText(node, source))
	if IsStarReExport(node, source) {
		return []map[string]any{reExportEntry(
			"*",
			"*",
			moduleSource,
			fullImportName,
			shared.NodeLine(sourceNode),
			lang,
		)}
	}

	specifiers := ReExportSpecifiers(node, source)
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

// ReExportSource returns the unquoted module specifier of a re-export statement
// (export { ... } from "m", export * from "m", export * as ns from "m",
// export type { ... } from "m"). It reads only the grammar's source field and
// accepts it only when that field is a single string literal; it returns an
// empty string for every other export_statement.
//
// There is deliberately no text fallback. A declaration export such as
// "export class X { ... }" has no source field, and scanning its text for
// " from " matched words inside comments, strings and template literals
// anywhere in the declaration body, minting a re-export whose module name ran
// to the end of the file (issue #7056).
func ReExportSource(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	sourceNode := node.ChildByFieldName("source")
	if sourceNode == nil || sourceNode.Kind() != "string" {
		return ""
	}
	return strings.Trim(strings.TrimSpace(shared.NodeText(sourceNode, source)), `"'`)
}

// ReExportSpecifier records one static export-clause mapping from a
// barrel's public name to the original symbol name in the target module.
type ReExportSpecifier struct {
	ExportedName string
	OriginalName string
	lineNumber   int
}

func reExportEntry(
	exportedName string,
	originalName string,
	moduleSource string,
	fullImportName string,
	lineNumber int,
	lang string,
) map[string]any {
	item := map[string]any{
		"name":             exportedName,
		"source":           moduleSource,
		"import_type":      "reexport",
		"full_import_name": fullImportName,
		"line_number":      lineNumber,
		"lang":             lang,
	}
	if originalName != "" {
		item["original_name"] = originalName
	}
	return item
}

// IsStarReExport reports whether an export_statement re-exports a whole
// module via the star form rather than a named export clause. It is decided
// structurally: a re-export node (one that has a module source) is a star
// re-export when it carries no export_clause child. This covers
//
//	export * from "..."            (source only)
//	export * as NS from "..."      (namespace_export child)
//	export type * from "..."       (type modifier; grammar emits an ERROR token)
//	export type * as NS from "..." (namespace_export child + ERROR token)
//
// The TypeScript tree-sitter grammar does not model the type modifier on a star
// export, so it produces an ERROR node for the "type" token; reading the node
// text for a leading "*" therefore misses the type-only forms. Named re-exports
// (export { A } from "...", export type { A } from "...") carry an export_clause
// and are not treated as star re-exports here so their per-name edges are kept.
func IsStarReExport(node *tree_sitter.Node, source []byte) bool {
	if node == nil {
		return false
	}
	if node.Kind() != "export_statement" {
		return false
	}
	if node.ChildByFieldName("source") == nil {
		return false
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "export_clause" {
			return false
		}
	}
	return true
}

// ReExportSpecifiers returns each "name as alias" mapping in an export clause,
// defaulting the exported name to the original name when no alias is present. It
// falls back to parsing the brace-delimited clause text when the grammar produces
// no export_specifier children.
func ReExportSpecifiers(node *tree_sitter.Node, source []byte) []ReExportSpecifier {
	specifiers := make([]ReExportSpecifier, 0)
	shared.WalkNamed(node, func(candidate *tree_sitter.Node) {
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
	if len(specifiers) > 0 {
		return specifiers
	}
	if isDeclarationExport(node) {
		return specifiers
	}
	return reExportSpecifiersFromText(node, source)
}

// isDeclarationExport reports whether an export_statement exports a
// declaration or a default value (export class/function/const ...,
// export default ...) rather than an export clause. Such a statement has no
// specifiers, and its braces delimit a declaration body, so the brace-text
// fallback must not read names out of it (issue #7056).
func isDeclarationExport(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	return node.ChildByFieldName("declaration") != nil || node.ChildByFieldName("value") != nil
}

func reExportSpecifiersFromText(
	node *tree_sitter.Node,
	source []byte,
) []ReExportSpecifier {
	text := strings.TrimSpace(shared.NodeText(node, source))
	start := strings.Index(text, "{")
	end := strings.Index(text, "}")
	if start < 0 || end <= start {
		return nil
	}

	parts := strings.Split(text[start+1:end], ",")
	specifiers := make([]ReExportSpecifier, 0, len(parts))
	for _, part := range parts {
		OriginalName, ExportedName := reExportSpecifierNames(part)
		if OriginalName == "" || ExportedName == "" {
			continue
		}
		specifiers = append(specifiers, ReExportSpecifier{
			ExportedName: ExportedName,
			OriginalName: OriginalName,
			lineNumber:   shared.NodeLine(node),
		})
	}
	return specifiers
}

func reExportSpecifierNames(raw string) (string, string) {
	part := strings.TrimSpace(strings.TrimPrefix(exportSpecifierWithoutLineComments(raw), "type "))
	if part == "" || strings.Contains(part, "...") {
		return "", ""
	}

	fields := strings.Fields(part)
	switch len(fields) {
	case 1:
		return fields[0], fields[0]
	case 3:
		if fields[1] == "as" {
			return fields[0], fields[2]
		}
	}

	left, right, ok := strings.Cut(part, " as ")
	if !ok {
		return "", ""
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return "", ""
	}
	return left, right
}

func exportSpecifierWithoutLineComments(raw string) string {
	segments := make([]string, 0, 1)
	for _, line := range strings.Split(exportSpecifierWithoutBlockComments(raw), "\n") {
		beforeComment, _, _ := strings.Cut(line, "//")
		if trimmed := strings.TrimSpace(beforeComment); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(segments, " "))
}

func exportSpecifierWithoutBlockComments(raw string) string {
	var cleaned strings.Builder
	cleaned.Grow(len(raw))
	for i := 0; i < len(raw); {
		if i+1 < len(raw) && raw[i] == '/' && raw[i+1] == '*' {
			cleaned.WriteByte(' ')
			i += 2
			for i+1 < len(raw) && (raw[i] != '*' || raw[i+1] != '/') {
				i++
			}
			if i+1 >= len(raw) {
				break
			}
			i += 2
			continue
		}
		cleaned.WriteByte(raw[i])
		i++
	}
	return cleaned.String()
}
