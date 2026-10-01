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
	statementTypeOnly := reExportStatementIsTypeOnly(fullImportName)
	if IsStarReExport(node, source) {
		return []map[string]any{reExportEntry(
			"*",
			"*",
			moduleSource,
			fullImportName,
			shared.NodeLine(sourceNode),
			lang,
			statementTypeOnly,
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
			statementTypeOnly || specifier.TypeOnly,
		))
	}
	return items
}

// reExportStatementIsTypeOnly reports whether a re-export statement is spelled
// `export type { … } from` or `export type * from`. It reads the statement text
// because the TypeScript grammar leaves the modifier on a star re-export as an
// ERROR token instead of a `type` child.
func reExportStatementIsTypeOnly(statement string) bool {
	// Comments may sit between the keywords (`export /* c */ type { Q }`), and
	// the statement text keeps them.
	statement = exportSpecifierWithoutLineComments(statement)
	rest, ok := strings.CutPrefix(strings.TrimSpace(statement), "export")
	if !ok {
		return false
	}
	rest, ok = strings.CutPrefix(strings.TrimSpace(rest), "type")
	if !ok || rest == "" {
		return false
	}
	switch rest[0] {
	case ' ', '\t', '\n', '\r', '{', '*':
		return true
	default:
		return false
	}
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
	// TypeOnly is true when the specifier itself carries the `type` modifier
	// (`export { type Z } from "m"`); a statement-level `export type` is read
	// from the statement text instead.
	TypeOnly   bool
	lineNumber int
}

func reExportEntry(
	exportedName string,
	originalName string,
	moduleSource string,
	fullImportName string,
	lineNumber int,
	lang string,
	typeOnly bool,
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
	if typeOnly {
		item[shared.ImportFlagTypeOnly] = true
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
		OriginalName, ok := moduleSpecifierName(nameNode, source)
		if !ok || OriginalName == "" {
			// A specifier needs a recordable original name: the reducer reads a
			// missing original as "the same as the exported name", which would
			// resolve export { '' as c } to the module's c (#7461).
			return
		}
		ExportedName := OriginalName
		if aliasNode != nil {
			// A string-literal alias may be empty ('as ""'); presence, not a
			// non-empty value, decides that an alias was written.
			if ExportedName, ok = moduleSpecifierName(aliasNode, source); !ok {
				return
			}
		}
		specifiers = append(specifiers, ReExportSpecifier{
			ExportedName: ExportedName,
			OriginalName: OriginalName,
			TypeOnly:     exportSpecifierIsTypeOnly(candidate, nameNode, aliasNode, source),
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
	parts, ok := braceClauseSpecifiers(text)
	if !ok {
		return nil
	}

	specifiers := make([]ReExportSpecifier, 0, len(parts))
	for _, part := range parts {
		OriginalName, ExportedName := reExportSpecifierNames(part)
		if OriginalName == "" || ExportedName == "" {
			continue
		}
		specifiers = append(specifiers, ReExportSpecifier{
			ExportedName: ExportedName,
			OriginalName: OriginalName,
			TypeOnly:     exportSpecifierTextIsTypeOnly(part),
			lineNumber:   shared.NodeLine(node),
		})
	}
	return specifiers
}

// exportSpecifierIsTypeOnly reports whether one export specifier carries the
// `type` modifier. The grammar reads `export { type as Y }` as a modifier plus a
// name `as` with no alias, but TypeScript 4.5 defines that spelling as the VALUE
// named type exported under the alias Y, so it is not type-only. Flagging it
// would let a cycle query drop a real runtime edge; a genuine type-only export
// of a binding named `as` (`{ type as as Y }`) still carries an alias and is
// flagged.
func exportSpecifierIsTypeOnly(specifier, nameNode, aliasNode *tree_sitter.Node, source []byte) bool {
	if !hasTypeModifier(specifier) {
		return false
	}
	return aliasNode != nil || strings.TrimSpace(shared.NodeText(nameNode, source)) != "as"
}

// exportSpecifierTextIsTypeOnly is the text-fallback counterpart of
// exportSpecifierIsTypeOnly: a `type ` prefix marks the specifier type-only
// unless the whole specifier is the three-word value spelling `type as Y`.
func exportSpecifierTextIsTypeOnly(raw string) bool {
	part := strings.TrimSpace(exportSpecifierWithoutLineComments(raw))
	if !strings.HasPrefix(part, "type ") {
		return false
	}
	fields := strings.Fields(part)
	return len(fields) != 3 || fields[1] != "as"
}

// reExportSpecifierNames reads the original and exported name of one specifier
// the brace-text fallback split out: `name`, or `name as alias`, where either
// name may be a quoted string (`'a b' as "c"`). It tokenizes outside quotes, so
// a name holding spaces or the word `as` stays one token. It returns two empty
// strings for any other shape.
func reExportSpecifierNames(raw string) (string, string) {
	part := strings.TrimSpace(strings.TrimPrefix(exportSpecifierWithoutLineComments(raw), "type "))
	if part == "" {
		return "", ""
	}
	tokens := tokensOutsideQuotes(part)
	for _, token := range tokens {
		// A rest element (`...rest`) is not a specifier; a quoted name may hold
		// dots (`'...'`), so only an unquoted token is checked.
		if token[0] != '\'' && token[0] != '"' && strings.Contains(token, "...") {
			return "", ""
		}
	}
	switch {
	case len(tokens) == 1:
		name, ok := unquoteModuleSpecifierName(tokens[0])
		if !ok {
			return "", ""
		}
		return name, name
	case len(tokens) == 3 && tokens[1] == "as":
		original, originalOK := unquoteModuleSpecifierName(tokens[0])
		exported, exportedOK := unquoteModuleSpecifierName(tokens[2])
		if !originalOK || !exportedOK {
			return "", ""
		}
		return original, exported
	}
	return "", ""
}

// exportSpecifierWithoutLineComments returns raw with its comments removed and
// its line breaks turned into spaces, trimmed. It reads outside quoted strings, so
// a comment marker inside a quoted name (`'a//b'`, `'a/*x*/b'`) stays part of the
// name instead of truncating or rewriting it.
func exportSpecifierWithoutLineComments(raw string) string {
	var cleaned strings.Builder
	cleaned.Grow(len(raw))
	for i := 0; i < len(raw); {
		switch c := raw[i]; {
		case c == '\'' || c == '"':
			end := skipQuoted(raw, i)
			cleaned.WriteString(raw[i:end])
			i = end
		case c == '/' && i+1 < len(raw) && (raw[i+1] == '/' || raw[i+1] == '*'):
			cleaned.WriteByte(' ')
			i = skipComment(raw, i)
		case c == '\n' || c == '\r':
			cleaned.WriteByte(' ')
			i++
		default:
			cleaned.WriteByte(c)
			i++
		}
	}
	return strings.TrimSpace(cleaned.String())
}
