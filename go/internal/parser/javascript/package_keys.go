// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"slices"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/project"
	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// This file stamps the cross-repository call keys of #7601. A producer's
// package export carries package_id and export_name; a consumer's call bound
// to a bare package import carries package_export_symbol. The reducer derives
// package:<package_id>#<export_name> from the producer side and resolves the
// call only when exactly one definition carries the key, so each rule here is
// written to leave a key off rather than name a symbol it cannot prove.

// packageKeyCallKinds are the call kinds that are real runtime references.
// Type references (typescript.type_reference) and function-value references
// stay unkeyed.
var packageKeyCallKinds = map[string]struct{}{
	"function_call":    {},
	"constructor_call": {},
	"jsx_component":    {},
}

// packageExportStamper stamps package_id and export_name on the exported
// top-level declarations of one file.
type packageExportStamper struct {
	packageName string
	// entryFile is true when the package manifest names this file as an entry
	// or export target. Only such a file's default export is the package's
	// default export.
	entryFile bool
	parents   *syntax.ParentLookup
}

func newPackageExportStamper(repoRoot string, path string, fileRootKinds []string, parents *syntax.ParentLookup) packageExportStamper {
	return packageExportStamper{
		packageName: project.NearestPackageName(repoRoot, path),
		entryFile: slices.Contains(fileRootKinds, "javascript.node_package_entrypoint") ||
			slices.Contains(fileRootKinds, "javascript.node_package_export"),
		parents: parents,
	}
}

// stamp adds the producer keys to item when node is a top-level exported
// declaration of a file owned by a named package. A nil item is ignored.
func (s packageExportStamper) stamp(item map[string]any, node *tree_sitter.Node) {
	if item == nil || s.packageName == "" {
		return
	}
	name, _ := item["name"].(string)
	exportName := s.exportName(node, name)
	if exportName == "" {
		return
	}
	item["package_id"] = s.packageName
	item["export_name"] = exportName
}

// exportName returns the name node is exported under, or "" when node is not
// a top-level export. Accepted shapes are `export function|class X`,
// `export default function|class X` (entry files only), and
// `export const|let|var X = <function>`. The export must sit directly under
// the program, so a member of a TypeScript namespace or `declare module` block
// is not a package export.
func (s packageExportStamper) exportName(node *tree_sitter.Node, name string) string {
	if node == nil || strings.TrimSpace(name) == "" {
		return ""
	}
	exportNode := s.parents.Parent(node)
	if node.Kind() == "variable_declarator" {
		if exportNode == nil || (exportNode.Kind() != "lexical_declaration" && exportNode.Kind() != "variable_declaration") {
			return ""
		}
		exportNode = s.parents.Parent(exportNode)
	}
	if exportNode == nil || exportNode.Kind() != "export_statement" {
		return ""
	}
	if program := s.parents.Parent(exportNode); program == nil || program.Kind() != "program" {
		return ""
	}
	if isDefaultExportStatement(exportNode) {
		if !s.entryFile {
			return ""
		}
		return "default"
	}
	return name
}

func isDefaultExportStatement(node *tree_sitter.Node) bool {
	for index := uint(0); index < node.ChildCount(); index++ {
		child := node.Child(index)
		if child != nil && !child.IsNamed() && child.Kind() == "default" {
			return true
		}
	}
	return false
}

// packageImportBinding is what one local name of a file is bound to by a bare
// package import. An empty exportName marks a namespace binding
// (`import * as ns`, `const ns = require("pkg")`), whose members are the
// package's named exports.
type packageImportBinding struct {
	source     string
	exportName string
}

// annotatePackageImportCalls sets package_export_symbol on each real call bound
// to a bare package import. Two shapes are keyed: `local(`, `new Local(`,
// `<Local />` for a named or default import, and `ns.member(` (also with new
// or JSX) for a namespace import. A deeper chain, a member of a non-namespace
// binding, and any name the file declares again are left unkeyed.
func annotatePackageImportCalls(payload map[string]any, root *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) {
	calls, _ := payload["function_calls"].([]map[string]any)
	if len(calls) == 0 {
		return
	}
	bindings := packageImportBindings(payload)
	if len(bindings) == 0 {
		return
	}

	type keyedCall struct {
		call      map[string]any
		localName string
		key       string
	}
	var keyed []keyedCall
	usedNames := map[string]packageImportBinding{}
	for _, call := range calls {
		callKind, _ := call["call_kind"].(string)
		if _, ok := packageKeyCallKinds[callKind]; !ok {
			continue
		}
		name, _ := call["name"].(string)
		fullName, _ := call["full_name"].(string)
		localName, exportName := name, ""
		if fullName == name {
			if binding, ok := bindings[name]; ok && binding.exportName != "" {
				exportName = binding.exportName
			}
		} else if namespace, member, ok := strings.Cut(fullName, "."); ok && member == name {
			if binding, ok := bindings[namespace]; ok && binding.exportName == "" && isJSIdentifier(member) {
				localName, exportName = namespace, member
			}
		}
		if exportName == "" {
			continue
		}
		binding := bindings[localName]
		usedNames[localName] = binding
		keyed = append(keyed, keyedCall{call: call, localName: localName, key: "package:" + binding.source + "#" + exportName})
	}
	if len(keyed) == 0 {
		return
	}

	redeclared := redeclaredImportNames(root, source, parents, usedNames)
	for _, item := range keyed {
		if _, ok := redeclared[item.localName]; ok {
			continue
		}
		item.call["package_export_symbol"] = item.key
	}
}

// packageImportBindings maps each local name bound by a bare package import to
// its binding. A local name bound more than once, or also bound by a
// type-only, relative, subpath, in-repo, or re-export row, is left out.
func packageImportBindings(payload map[string]any) map[string]packageImportBinding {
	imports, _ := payload["imports"].([]map[string]any)
	bindings := map[string]packageImportBinding{}
	blocked := map[string]struct{}{}
	for _, entry := range imports {
		name, _ := entry["name"].(string)
		alias, hasAlias := entry["alias"].(string)
		source, _ := entry["source"].(string)
		importType, _ := entry["import_type"].(string)
		if !hasAlias && importType == "" && name == source {
			continue // side-effect import (`import "pkg"`): binds no name
		}
		localName := name
		if alias != "" {
			localName = alias
		}
		if !isJSIdentifier(localName) {
			continue
		}
		binding := packageImportBinding{source: source, exportName: name}
		if name == "*" {
			binding.exportName = ""
		}
		resolved, _ := entry["resolved_source"].(string)
		typeOnly, _ := entry[shared.ImportFlagTypeOnly].(bool)
		eligible := !typeOnly && importType != "reexport" && strings.TrimSpace(resolved) == "" &&
			isBarePackageSpecifier(source) && (binding.exportName == "" || isJSIdentifier(binding.exportName))
		existing, seen := bindings[localName]
		_, isBlocked := blocked[localName]
		switch {
		case isBlocked:
		case !eligible || (seen && existing != binding):
			blocked[localName] = struct{}{}
			delete(bindings, localName)
		default:
			bindings[localName] = binding
		}
	}
	return bindings
}

// redeclaredImportNames returns the names in used that the file binds again
// anywhere outside their own import: a parameter, a variable, a function or
// class name, a catch parameter, or a destructuring pattern. The rule is
// file-wide on purpose. It misses a call that sits outside the shadowing scope,
// but it never keys a call that resolves to a local binding.
func redeclaredImportNames(
	root *tree_sitter.Node,
	source []byte,
	parents *syntax.ParentLookup,
	used map[string]packageImportBinding,
) map[string]struct{} {
	redeclared := map[string]struct{}{}
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "identifier", "type_identifier", "shorthand_property_identifier_pattern":
		default:
			return
		}
		// The conversion inside the index expression does not allocate.
		binding, ok := used[string(source[node.StartByte():node.EndByte()])]
		if !ok || !isBindingIdentifier(node, parents) {
			return
		}
		text := string(source[node.StartByte():node.EndByte()])
		if requireSource, ok := requireDeclaratorSource(node, source, parents); ok && requireSource == binding.source {
			return // the require declarator that created the binding
		}
		redeclared[text] = struct{}{}
	})
	return redeclared
}

// isBindingIdentifier reports whether node declares a name rather than reading
// one. Import specifiers are not listed: an import cannot shadow itself.
func isBindingIdentifier(node *tree_sitter.Node, parents *syntax.ParentLookup) bool {
	if node.Kind() == "shorthand_property_identifier_pattern" {
		return true
	}
	parent := parents.Parent(node)
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "formal_parameters", "array_pattern", "rest_pattern":
		return true
	case "variable_declarator", "function_declaration", "generator_function_declaration",
		"function_expression", "function", "generator_function", "function_signature",
		"class_declaration", "abstract_class_declaration", "class", "enum_declaration", "internal_module":
		return isFieldChild(parent, "name", node)
	case "required_parameter", "optional_parameter":
		return isFieldChild(parent, "pattern", node)
	case "arrow_function", "catch_clause":
		return isFieldChild(parent, "parameter", node)
	case "assignment_pattern", "object_assignment_pattern", "for_in_statement":
		return isFieldChild(parent, "left", node)
	case "pair_pattern":
		return isFieldChild(parent, "value", node)
	}
	return false
}

func isFieldChild(parent *tree_sitter.Node, field string, node *tree_sitter.Node) bool {
	child := parent.ChildByFieldName(field)
	return child != nil && child.Id() == node.Id()
}

// requireDeclaratorSource returns the module of the require call that a
// binding identifier is declared by (`const x = require("m")`,
// `const { x } = require("m")`, `const x = require("m").x`), and false for any
// other binding.
func requireDeclaratorSource(node *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) (string, bool) {
	current := parents.Parent(node)
	for current != nil {
		switch current.Kind() {
		case "object_pattern", "pair_pattern", "array_pattern", "rest_pattern", "object_assignment_pattern":
			current = parents.Parent(current)
			continue
		case "variable_declarator":
			value := current.ChildByFieldName("value")
			if moduleSource, ok := syntax.RequireModuleSource(value, source); ok {
				return moduleSource, true
			}
			if value != nil && value.Kind() == "member_expression" {
				return syntax.RequireModuleSource(value.ChildByFieldName("object"), source)
			}
		}
		return "", false
	}
	return "", false
}

// isBarePackageSpecifier reports whether specifier names a whole package
// (`pkg` or `@scope/pkg`) rather than a relative path, an absolute path, a
// subpath (`pkg/sub`), a Node.js subpath import (`#x`), or a URL or protocol
// form (`node:fs`).
func isBarePackageSpecifier(specifier string) bool {
	if specifier == "" || strings.ContainsAny(specifier, " \t\r\n\\:") {
		return false
	}
	switch specifier[0] {
	case '.', '/', '#':
		return false
	}
	if scope, name, ok := strings.Cut(specifier, "/"); ok {
		return strings.HasPrefix(scope, "@") && len(scope) > 1 && name != "" && !strings.Contains(name, "/")
	}
	return !strings.HasPrefix(specifier, "@")
}

// isJSIdentifier reports whether name is one plain identifier: a letter, `_`,
// or `$`, then letters, digits, `_`, or `$`. Raw call text with whitespace,
// comments, optional chaining, or parentheses fails it.
func isJSIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index, r := range name {
		switch {
		case r == '_' || r == '$' || unicode.IsLetter(r):
		case index > 0 && unicode.IsDigit(r):
		default:
			return false
		}
	}
	return true
}
