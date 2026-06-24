// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// handleImportCall records a use/import/alias/require row, expanding alias brace
// groups, and feeds dead-code use facts from the AST node text.
func (e *elixirExtractor) handleImportCall(
	node *tree_sitter.Node,
	keyword string,
	moduleScope elixirModuleScope,
) {
	paths := elixirImportPaths(node, keyword, e.source)
	if len(paths) == 0 {
		return
	}
	lineNumber := shared.NodeLine(node)
	for _, path := range paths {
		var aliasName any
		if keyword == "alias" && path != "" {
			aliasName = lastAliasSegment(path)
		}
		shared.AppendBucket(e.payload, "imports", map[string]any{
			"name":             path,
			"full_import_name": keyword + " " + path,
			"line_number":      lineNumber,
			"alias":            aliasName,
			"lang":             "elixir",
			"is_dependency":    e.isDependency,
			"import_type":      keyword,
		})
	}
	if moduleScope.name != "" {
		recordElixirUse(e.facts, moduleScope.name, elixirNodeText(node, e.source), keyword, paths)
	}
}

// handleAttribute records a module-attribute variable row from an @attr node and
// returns whether the node was consumed. @doc, @moduledoc, @impl, and
// @behaviour feed metadata rather than producing variable rows.
func (e *elixirExtractor) handleAttribute(
	node *tree_sitter.Node,
	moduleScope elixirModuleScope,
) bool {
	name, value, ok := elixirAttribute(node, e.source)
	if !ok {
		return false
	}
	if name == "@behaviour" && moduleScope.name != "" {
		recordElixirBehaviour(e.facts, moduleScope.name, value)
	}
	// @doc and @moduledoc are documentation, not variables; every other module
	// attribute (including @impl and @behaviour) becomes a variable row to match
	// the prior extractor.
	if name == "@doc" || name == "@moduledoc" {
		return true
	}
	item := map[string]any{
		"name":           name,
		"line_number":    shared.NodeLine(node),
		"end_line":       shared.NodeEndLine(node),
		"lang":           "elixir",
		"is_dependency":  e.isDependency,
		"value":          value,
		"attribute_kind": "module_attribute",
	}
	if moduleScope.name != "" {
		item["context"] = []any{moduleScope.name, "module", moduleScope.line}
		item["context_type"] = "module"
		item["class_context"] = moduleScope.name
	}
	if e.options.IndexSource {
		item["source"] = elixirNodeSource(node, e.source)
	}
	shared.AppendBucket(e.payload, "variables", item)
	return true
}

// handleExpressionCall records a function-call row for a call node in an
// expression position, then recurses into argument subtrees for nested calls.
func (e *elixirExtractor) handleExpressionCall(
	node *tree_sitter.Node,
	moduleScope elixirModuleScope,
	fnScope *elixirFunctionScope,
) {
	// Only invocations with a parenthesized argument list are calls. Bare
	// references, dotted field access (`state.items`), and control-flow special
	// forms (`case value do`, `for x <- ...`) carry no parenthesized arguments and
	// were never matched by the prior `name(` extraction.
	if !elixirCallHasParenArguments(node, e.source) {
		return
	}
	receiver, name := elixirCallTarget(node, e.source)
	if name == "" {
		return
	}
	switch name {
	case "def", "defp", "do", "fn":
		return
	}
	fullName := name
	if receiver != "" {
		fullName = receiver + "." + name
	}
	args := elixirCallArgumentTexts(node, e.source)
	e.appendCall(node, name, fullName, receiver, args, moduleScope, fnScope)
}

// appendCall appends a unique function-call row keyed by full name and attaches
// enclosing function or module context from the active scopes.
func (e *elixirExtractor) appendCall(
	node *tree_sitter.Node,
	name string,
	fullName string,
	receiver string,
	args []string,
	moduleScope elixirModuleScope,
	fnScope *elixirFunctionScope,
) {
	if strings.TrimSpace(fullName) == "" {
		return
	}
	if _, ok := e.seenCalls[fullName]; ok {
		return
	}
	e.seenCalls[fullName] = struct{}{}

	item := map[string]any{
		"name":          name,
		"full_name":     fullName,
		"line_number":   shared.NodeLine(node),
		"args":          args,
		"lang":          "elixir",
		"is_dependency": e.isDependency,
	}
	if receiver != "" {
		item["inferred_obj_type"] = receiver
	} else {
		item["inferred_obj_type"] = nil
	}
	if fnScope != nil {
		item["context"] = []any{fnScope.name, "function", fnScope.line}
	} else if moduleScope.name != "" {
		item["context"] = []any{moduleScope.name, "module", moduleScope.line}
	}
	if moduleScope.name != "" {
		item["class_context"] = moduleScope.name
	}
	shared.AppendBucket(e.payload, "function_calls", item)
}

// appendGuardCalls records the helper calls inside a defguard guard expression so
// guard predicates contribute call evidence without re-walking the signature.
func (e *elixirExtractor) appendGuardCalls(
	node *tree_sitter.Node,
	target *tree_sitter.Node,
	guardName string,
	moduleScope elixirModuleScope,
	defLine int,
) {
	guard := elixirGuardExpression(node, target)
	if guard == nil {
		return
	}
	shared.WalkNamed(guard, func(call *tree_sitter.Node) {
		if call.Kind() != "call" {
			return
		}
		receiver, name := elixirCallTarget(call, e.source)
		if name == "" || name == guardName {
			return
		}
		fullName := name
		if receiver != "" {
			fullName = receiver + "." + name
		}
		args := elixirCallArgumentTexts(call, e.source)
		fnScope := &elixirFunctionScope{name: guardName, line: defLine}
		e.appendCall(call, name, fullName, receiver, args, moduleScope, fnScope)
	})
}

// markFunctionDynamicDispatch flags a function whose body performs an apply/3
// dynamic dispatch so reachability stays conservative about the target.
func (e *elixirExtractor) markFunctionDynamicDispatch(node *tree_sitter.Node, item map[string]any) {
	found := false
	shared.WalkNamed(node, func(call *tree_sitter.Node) {
		if found || call.Kind() != "call" {
			return
		}
		if _, name := elixirCallTarget(call, e.source); name == "apply" {
			found = true
		}
	})
	if found {
		item["exactness_blockers"] = appendElixirMetadataString(
			item["exactness_blockers"],
			"dynamic_dispatch_unresolved",
		)
	}
}
