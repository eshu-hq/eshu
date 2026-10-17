// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"bytes"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// This file holds the closed import-name rule of the #8280 static member keys
// (review F10, ruling (B)). The #7601 keys keep their own open rule
// (redeclaredImportNames), unchanged; #8310 tracks closing it.

// nonVariableKinds are the node kinds whose text is never the variable: a
// property name, a string, template, or JSX text, a comment, a regular
// expression, and a statement label. A token at such a node is not an
// occurrence of the name.
var nonVariableKinds = map[string]struct{}{
	"property_identifier":  {},
	"string_fragment":      {},
	"comment":              {},
	"regex_pattern":        {},
	"statement_identifier": {},
	"jsx_text":             {},
}

// closedMemberImportNames reports, for each local import name in names,
// whether every occurrence of the name in the file is one the closed rule
// allows (allowedImportOccurrence): the import itself, a call, `new`, or JSX
// use of the name or of a member chain on it, a TypeScript type position, or
// a read that runs no user code. Any other occurrence, in any node kind (a
// binding of any kind, an import alias, a using declaration, an assignment, a
// property write, a plain read that could hand the class to code that
// replaces C.m), makes the name false. A file with a with statement, a `\u`
// escape (which can spell the name), or the identifier eval (a sloppy-mode
// eval can declare a var) makes every name false. The source is scanned once.
func closedMemberImportNames(
	root *tree_sitter.Node,
	source []byte,
	parents *syntax.ParentLookup,
	names map[string]packageImportBinding,
) map[string]bool {
	allowed := map[string]bool{}
	if len(names) == 0 || hasWithStatement(root, source) || bytes.Contains(source, []byte(`\u`)) {
		return allowed
	}
	disqualified := map[string]bool{}
	for start := 0; start < len(source); {
		if !isJSIdentifierByte(source[start]) {
			start++
			continue
		}
		end := start + 1
		for end < len(source) && isJSIdentifierByte(source[end]) {
			end++
		}
		token := source[start:end]
		tokenStart := start
		start = end
		if token[0] >= '0' && token[0] <= '9' {
			continue
		}
		name := string(token)
		binding, isName := names[name]
		if !isName && name != "eval" {
			continue
		}
		node := root.NamedDescendantForByteRange(uint(tokenStart), uint(end))
		if node == nil || node.StartByte() != uint(tokenStart) || node.EndByte() != uint(end) {
			continue
		}
		if _, notVariable := nonVariableKinds[node.Kind()]; notVariable {
			continue
		}
		if !isName {
			if node.Kind() == "identifier" {
				return map[string]bool{}
			}
			continue
		}
		if !allowedImportOccurrence(node, binding, source, parents) {
			disqualified[name] = true
		}
	}
	for name := range names {
		allowed[name] = !disqualified[name]
	}
	return allowed
}

// allowedImportOccurrence reports whether one occurrence of an import's local
// name keeps the closed rule: it sits in the import statement or in the
// require declarator of the same module, in a TypeScript type position, in a
// call, `new`, or JSX tag name (directly or as the object of a member chain
// that ends in one), or in a read that runs no user code (safeRead).
func allowedImportOccurrence(node *tree_sitter.Node, binding packageImportBinding, source []byte, parents *syntax.ParentLookup) bool {
	if inTypePosition(node, parents) {
		return true
	}
	switch node.Kind() {
	case "identifier", "type_identifier", "shorthand_property_identifier_pattern":
	default:
		return false
	}
	if hasAncestorKind(node, "import_statement", parents) {
		return true
	}
	if requireSource, ok := requireDeclaratorSource(node, source, parents); ok && requireSource == binding.source {
		return true
	}
	parent := parents.Parent(node)
	if parent == nil {
		return false
	}
	if callableUse(parent, node) {
		return true
	}
	if parent.Kind() == "member_expression" && isFieldChild(parent, "object", node) {
		top := parent
		for {
			up := parents.Parent(top)
			if up == nil || up.Kind() != "member_expression" || !isFieldChild(up, "object", top) {
				return up != nil && callableUse(up, top)
			}
			top = up
		}
	}
	return safeRead(node, parent, parents)
}

// callableUse reports whether node is the function of a call, the
// constructor of `new`, or the tag name of a JSX element.
func callableUse(parent *tree_sitter.Node, node *tree_sitter.Node) bool {
	switch parent.Kind() {
	case "call_expression":
		return isFieldChild(parent, "function", node)
	case "new_expression":
		return isFieldChild(parent, "constructor", node)
	case "jsx_opening_element", "jsx_self_closing_element", "jsx_closing_element":
		return isFieldChild(parent, "name", node)
	}
	return false
}

// safeRead reports whether node, an identifier read, sits in a position that
// runs no user code and lets no value of it escape: the condition of an if,
// while, or do statement or of a ternary (ToBoolean never calls a method of an
// object), the operand of `!` or `typeof`, or an operand of `===` or `!==`
// (strict equality never coerces). Every other read (an argument, an operand
// of `||`, `??`, `&&`, `+`, or `==`, a spread, an initializer, an extends
// clause) can hand the value to code that replaces a member, so it does not
// keep the rule.
func safeRead(node *tree_sitter.Node, parent *tree_sitter.Node, parents *syntax.ParentLookup) bool {
	switch parent.Kind() {
	case "parenthesized_expression":
		statement := parents.Parent(parent)
		if statement == nil || !isFieldChild(statement, "condition", parent) {
			return false
		}
		switch statement.Kind() {
		case "if_statement", "while_statement", "do_statement":
			return true
		}
	case "ternary_expression":
		return isFieldChild(parent, "condition", node)
	case "unary_expression":
		switch fieldTokenKind(parent, "operator") {
		case "!", "typeof":
			return true
		}
	case "binary_expression":
		switch fieldTokenKind(parent, "operator") {
		case "===", "!==":
			return true
		}
	}
	return false
}

// fieldTokenKind returns the kind of the child at field, the operator token
// of a unary or binary expression, or "" when there is none.
func fieldTokenKind(node *tree_sitter.Node, field string) string {
	if child := node.ChildByFieldName(field); child != nil {
		return child.Kind()
	}
	return ""
}

// inTypePosition reports whether node sits in a TypeScript type that the
// compiler erases: a type annotation, type arguments, a type query, an
// implements clause, an interface extends clause, or a type alias (a type
// alias of the same name binds a type, never the value). A class or interface
// name is a declaration, not a type position.
func inTypePosition(node *tree_sitter.Node, parents *syntax.ParentLookup) bool {
	for current := parents.Parent(node); current != nil; current = parents.Parent(current) {
		switch current.Kind() {
		case "type_annotation", "type_arguments", "type_query", "implements_clause", "extends_type_clause",
			"type_alias_declaration":
			return true
		case "statement_block", "program", "expression_statement", "lexical_declaration", "class_body":
			return false
		}
	}
	return false
}

func hasAncestorKind(node *tree_sitter.Node, kind string, parents *syntax.ParentLookup) bool {
	for current := parents.Parent(node); current != nil; current = parents.Parent(current) {
		if current.Kind() == kind {
			return true
		}
	}
	return false
}

// isJSIdentifierByte reports whether b can be part of a JavaScript identifier
// token: an ASCII letter, digit, '_', or '$', or any byte of a multi-byte
// UTF-8 sequence.
func isJSIdentifierByte(b byte) bool {
	return b == '_' || b == '$' || b >= 0x80 || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
