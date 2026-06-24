// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// elixirFunctionSpan records the AST span of an Elixir function definition. It
// keys function metadata by node start and end lines rather than a text line
// index.
type elixirFunctionSpan struct {
	keyword    string
	name       string
	moduleName string
	args       []string
	startLine  int
	endLine    int
}

// elixirNamedChildren returns the named children of a node as a stable slice.
func elixirNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

// elixirCallHead returns the leading identifier of a call node, which is the
// keyword (def, defmodule, alias) or bare function name.
func elixirCallHead(node *tree_sitter.Node, source []byte) string {
	children := elixirNamedChildren(node)
	if len(children) == 0 {
		return ""
	}
	child := children[0]
	switch child.Kind() {
	case "identifier", "operator_identifier":
		return strings.TrimSpace(shared.NodeText(&child, source))
	default:
		return ""
	}
}

// elixirDefinitionName returns the alias or identifier named by a defmodule,
// defprotocol, or defimpl call's first argument.
func elixirDefinitionName(node *tree_sitter.Node, source []byte) string {
	for _, child := range elixirNamedChildren(node) {
		child := child
		if child.Kind() != "arguments" {
			continue
		}
		for _, argument := range elixirNamedChildren(&child) {
			argument := argument
			switch argument.Kind() {
			case "alias", "identifier":
				return strings.TrimSpace(shared.NodeText(&argument, source))
			}
		}
	}
	return ""
}

// elixirDefinitionTargetCall returns the node that names a function definition.
// The target may be a call (parenthesized signature) or a bare identifier
// (no-arg signature), sitting directly in the arguments or under a
// binary_operator when the definition carries a `when` guard.
func elixirDefinitionTargetCall(node *tree_sitter.Node) *tree_sitter.Node {
	for _, child := range elixirNamedChildren(node) {
		child := child
		if child.Kind() != "arguments" {
			continue
		}
		for _, argument := range elixirNamedChildren(&child) {
			argument := argument
			if target := elixirDefinitionTargetArgument(&argument); target != nil {
				target := *target
				return &target
			}
		}
	}
	return nil
}

// elixirDefinitionTargetArgument returns the call or identifier node naming a
// function from a single definition argument, descending through a `when` guard
// binary_operator. The first argument of a definition is always the name; a
// bare identifier means a no-argument function.
func elixirDefinitionTargetArgument(argument *tree_sitter.Node) *tree_sitter.Node {
	if argument == nil {
		return nil
	}
	switch argument.Kind() {
	case "call", "identifier":
		return argument
	case "binary_operator":
		for _, child := range elixirNamedChildren(argument) {
			child := child
			if target := elixirDefinitionTargetArgument(&child); target != nil {
				return target
			}
		}
	}
	return nil
}

// elixirDefinitionTargetName returns the function name for a definition target,
// handling both parenthesized call targets and bare identifier targets.
func elixirDefinitionTargetName(target *tree_sitter.Node, source []byte) string {
	if target == nil {
		return ""
	}
	if target.Kind() == "identifier" {
		return strings.TrimSpace(shared.NodeText(target, source))
	}
	return elixirCallHead(target, source)
}

// elixirCallArgumentTexts returns the trimmed source text of each named argument
// to a call node, used for function parameters and call arguments.
func elixirCallArgumentTexts(node *tree_sitter.Node, source []byte) []string {
	for _, child := range elixirNamedChildren(node) {
		child := child
		if child.Kind() != "arguments" {
			continue
		}
		args := make([]string, 0)
		for _, argument := range elixirNamedChildren(&child) {
			argument := argument
			text := strings.TrimSpace(shared.NodeText(&argument, source))
			if text != "" {
				args = append(args, text)
			}
		}
		return args
	}
	return []string{}
}

// elixirModuleKeyword reports whether keyword introduces a module-like
// definition.
func elixirModuleKeyword(keyword string) bool {
	switch keyword {
	case "defmodule", "defprotocol", "defimpl":
		return true
	default:
		return false
	}
}

// elixirFunctionKeyword reports whether keyword introduces a function-like
// definition.
func elixirFunctionKeyword(keyword string) bool {
	switch keyword {
	case "def", "defp", "defmacro", "defmacrop", "defdelegate", "defguard", "defguardp":
		return true
	default:
		return false
	}
}

// moduleKind maps a module-definition keyword to its payload module_kind value.
func moduleKind(keyword string) string {
	switch keyword {
	case "defprotocol":
		return "protocol"
	case "defimpl":
		return "protocol_implementation"
	default:
		return "module"
	}
}

// functionSemanticKind maps a function-definition keyword to its payload
// semantic_kind value.
func functionSemanticKind(keyword string) string {
	switch keyword {
	case "defmacro", "defmacrop":
		return "macro"
	case "defdelegate":
		return "delegate"
	case "defguard", "defguardp":
		return "guard"
	default:
		return "function"
	}
}
