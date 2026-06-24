// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// cppFunctionNameAndClass resolves the method name and enclosing class for a
// function_definition node. It first tries the out-of-line qualified declarator
// (`Class::method`) read from AST fields, then falls back to the plain
// declarator identifier plus the nearest enclosing class/struct ancestor for
// in-class definitions and free functions.
func cppFunctionNameAndClass(node *tree_sitter.Node, nameNode *tree_sitter.Node, source []byte) (string, string) {
	if name, classContext := cppQualifiedFunctionNameAndClassFromNode(node, source); name != "" {
		return name, classContext
	}
	return strings.TrimSpace(shared.NodeText(nameNode, source)),
		strings.TrimSpace(nearestNamedAncestor(node, source, "class_specifier", "struct_specifier"))
}

// cppQualifiedFunctionNameAndClassFromNode recovers the method name and its
// enclosing class/scope from an out-of-line function_definition such as
// `Class::method`, `Class::~Class`, `Class::operator==`, or a nested
// `Outer::Inner::method`. It reads the tree-sitter declarator fields rather than
// scanning node text, so destructor, operator, and template-qualified
// definitions the old regex could not match are recovered at byte-parity with
// the source.
//
// The class context mirrors the historical "innermost scope before the final
// name" contract: for `api::Service::run` the class is `Service`, matching the
// class declared in the corresponding header, and for non-method definitions it
// returns empty so the caller falls back to the enclosing class/struct ancestor.
func cppQualifiedFunctionNameAndClassFromNode(node *tree_sitter.Node, source []byte) (string, string) {
	declarator := cppFunctionDeclarator(node.ChildByFieldName("declarator"))
	if declarator == nil {
		return "", ""
	}
	qualified := declarator.ChildByFieldName("declarator")
	if qualified == nil || qualified.Kind() != "qualified_identifier" {
		return "", ""
	}
	innermost := cppInnermostQualifiedIdentifier(qualified)
	scope := innermost.ChildByFieldName("scope")
	name := innermost.ChildByFieldName("name")
	// A qualified_identifier normally carries both scope and name fields; guard
	// against partial parses on malformed input.
	if scope == nil || name == nil {
		return "", ""
	}
	functionName := strings.TrimSpace(shared.NodeText(name, source))
	classContext := cppQualifiedScopeName(scope, source)
	if functionName == "" || classContext == "" {
		return "", ""
	}
	return functionName, classContext
}

// cppFunctionDeclarator unwraps pointer and reference declarator nodes that the
// grammar nests around the function_declarator when an out-of-line definition
// returns a pointer or reference (`R* C::m()`, `R& C::m()`). It returns the
// function_declarator, or nil when the declarator is not a function definition
// declarator (for example a free function with a plain identifier declarator).
func cppFunctionDeclarator(node *tree_sitter.Node) *tree_sitter.Node {
	for node != nil {
		switch node.Kind() {
		case "function_declarator":
			return node
		case "pointer_declarator", "reference_declarator":
			node = cppInnerDeclarator(node)
		default:
			return nil
		}
	}
	return nil
}

// cppInnerDeclarator returns the wrapped declarator of a pointer or reference
// declarator. The grammar attaches it to the `declarator` field for pointer
// declarators but as a positional named child for reference declarators, so this
// checks the field first and then falls back to the first nested declarator.
func cppInnerDeclarator(node *tree_sitter.Node) *tree_sitter.Node {
	if inner := node.ChildByFieldName("declarator"); inner != nil {
		return inner
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		switch child.Kind() {
		case "function_declarator", "pointer_declarator", "reference_declarator":
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// cppInnermostQualifiedIdentifier descends a chain of nested qualified
// identifiers (`Outer::Inner::name`) through the `name` field to the deepest
// `qualified_identifier`, whose `scope` is the immediate enclosing class.
func cppInnermostQualifiedIdentifier(node *tree_sitter.Node) *tree_sitter.Node {
	for {
		name := node.ChildByFieldName("name")
		if name == nil || name.Kind() != "qualified_identifier" {
			return node
		}
		node = name
	}
}

// cppQualifiedScopeName returns the bare class/scope name from a scope node,
// stripping any template argument list so `Box<T>` yields `Box`.
func cppQualifiedScopeName(scope *tree_sitter.Node, source []byte) string {
	if scope.Kind() == "template_type" {
		if nameNode := scope.ChildByFieldName("name"); nameNode != nil {
			return strings.TrimSpace(shared.NodeText(nameNode, source))
		}
	}
	return strings.TrimSpace(shared.NodeText(scope, source))
}
