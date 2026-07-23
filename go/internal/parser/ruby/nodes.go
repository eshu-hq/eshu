// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// This file holds the tree-sitter node accessors used by the Ruby AST walk:
// trimmed node text, constant and superclass name resolution, method parameter
// normalization, and the typed first-argument lookups for imports and module
// inclusions. They operate on nodes produced by the maintained
// tree-sitter/tree-sitter-ruby grammar; none of them scan source text for
// symbols.

// rubyVisibilityKeyword returns the canonical visibility keyword when value is
// exactly public, private, or protected, else the empty string.
func rubyVisibilityKeyword(value string) string {
	switch strings.TrimSpace(value) {
	case "public", "private", "protected":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

// text returns the trimmed source text spanned by node, or the empty string
// when node is nil.
func (s *rubySyntax) text(node *tree_sitter.Node) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(node, s.source))
}

// rawLine returns the 1-based source line, or the empty string when line is out
// of range. It backs the optional IndexSource definition-line capture.
func (s *rubySyntax) rawLine(line int) string {
	if line <= 0 || line > len(s.lines) {
		return ""
	}
	return s.lines[line-1]
}

// constantName returns the last `::` segment of a constant or scope_resolution
// node, matching the legacy last-segment behavior.
func (s *rubySyntax) constantName(node *tree_sitter.Node) string {
	if node == nil {
		return ""
	}
	return shared.LastPathSegment(s.text(node), "::")
}

// qualifiedClassName returns the class's namespace-qualified name (#5376 F3):
// the enclosing module/class scope prefix joined to the class's own declared
// name. The raw name-node text is used (not the last-segment collapse) so a
// compact "class Admin::Base" spelling keeps its qualifier. A leading "::"
// marks an absolute constant and ignores the enclosing path. Returns "" when
// the name node is absent.
func (s *rubySyntax) qualifiedClassName(node *tree_sitter.Node, scopeStack []rubyScope) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	raw := strings.TrimSpace(s.text(nameNode))
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "::") {
		return strings.TrimPrefix(raw, "::")
	}
	prefix := rubyScopeQualifiedPrefix(scopeStack)
	if prefix == "" {
		return raw
	}
	return prefix + "::" + raw
}

// rubyScopeQualifiedPrefix joins the names of the enclosing module and class
// scopes with "::" to form the namespace prefix for a nested definition. Method
// and singleton-class scopes are skipped: they are not part of a constant path.
func rubyScopeQualifiedPrefix(scopeStack []rubyScope) string {
	segments := make([]string, 0, len(scopeStack))
	for _, scope := range scopeStack {
		if scope.kind == rubyScopeModule || scope.kind == rubyScopeClass {
			if name := strings.TrimSpace(scope.name); name != "" {
				segments = append(segments, name)
			}
		}
	}
	return strings.Join(segments, "::")
}

// superclassName returns the base constant name from a (superclass) node.
func (s *rubySyntax) superclassName(node *tree_sitter.Node) string {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			switch child.Kind() {
			case "constant", "scope_resolution":
				return shared.LastPathSegment(s.text(child), "::")
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}

// superclassQualifiedName returns the full, possibly module-qualified base
// constant name from a (superclass) node (e.g. "ActionController::Base"),
// unlike superclassName which collapses the same node down to its last path
// segment ("Base") for the class-relationship "bases" fact. The full
// qualification is required by the Rails controller superclass-chain walk in
// dead_code_roots.go, which must not conflate an accepted qualified base
// (ActionController::Base) with an unrelated class that merely shares the
// same last segment (e.g. Sinatra::Base).
//
// A leading "::" (e.g. "class OrdersController < ::Base") is PRESERVED, not
// stripped: it is Ruby's absolute-constant-path marker, meaning the reference
// resolves starting at Object with NO enclosing-namespace search — a
// different resolution rule than the bare, relative "Base", which real Ruby
// searches via Module.nesting outward. Collapsing both spellings to the
// identical "Base" string would make them indistinguishable once persisted in
// qualified_bases, which previously let the reducer's #5500
// lexical-scope-aware candidate restriction (go/internal/rubycontroller)
// wrongly apply namespace-prefixed lexical search to an absolute reference,
// exact-matching an unrelated in-corpus class that merely shares the
// referencing class's own enclosing namespace and last segment (#5733 P1).
// rubycontroller.classNamespaceOf / normalizeBases detect and strip this
// marker to decide whether the namespace search runs at all; they never see a
// literal "::" survive into a Registry lookup.
func (s *rubySyntax) superclassQualifiedName(node *tree_sitter.Node) string {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			switch child.Kind() {
			case "constant", "scope_resolution":
				return s.text(child)
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}

// methodArguments returns normalized parameter names from a (method_parameters)
// node, matching legacy argument normalization.
func (s *rubySyntax) methodArguments(node *tree_sitter.Node) []string {
	if node == nil {
		return []string{}
	}
	args := make([]string, 0)
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return args
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			if name := s.parameterName(child); name != "" {
				args = append(args, name)
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return args
}

// parameterName returns the bound name of a single method parameter node,
// unwrapping optional, keyword, splat, and block parameter forms.
func (s *rubySyntax) parameterName(node *tree_sitter.Node) string {
	switch node.Kind() {
	case "identifier":
		return s.text(node)
	case "optional_parameter", "keyword_parameter", "splat_parameter",
		"hash_splat_parameter", "block_parameter", "splat_argument":
		if name := node.ChildByFieldName("name"); name != nil {
			return s.text(name)
		}
		return rubyNormalizeArgument(s.text(node))
	default:
		return rubyNormalizeArgument(s.text(node))
	}
}

// firstStringArgument returns the content of the first string argument of a
// call node, used to resolve require/require_relative/load import targets.
func (s *rubySyntax) firstStringArgument(node *tree_sitter.Node) string {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "string" {
			return s.stringContent(child)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}

// firstConstantArgument returns the last `::` segment of the first constant
// argument of a call node, used to resolve the module named by `include`.
func (s *rubySyntax) firstConstantArgument(node *tree_sitter.Node) string {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && (child.Kind() == "constant" || child.Kind() == "scope_resolution") {
			return shared.LastPathSegment(s.text(child), "::")
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}

// stringContent returns the (string_content) child text of a (string) node.
func (s *rubySyntax) stringContent(node *tree_sitter.Node) string {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "string_content" {
			return s.text(child)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}
