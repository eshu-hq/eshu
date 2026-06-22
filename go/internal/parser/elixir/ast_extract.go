package elixir

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// elixirExtractor walks the Elixir tree-sitter AST and emits every payload
// bucket directly from node ranges. It replaces the former line-scan and regex
// extraction so module, function, import, attribute, and call evidence is keyed
// by AST spans rather than text-split lines.
type elixirExtractor struct {
	payload      map[string]any
	source       []byte
	isDependency bool
	options      shared.Options
	facts        elixirDeadCodeFacts
	seenCalls    map[string]struct{}
	functions    []elixirFunctionSpan
}

// elixirModuleScope carries the enclosing module identity discovered while
// descending into a definition body. Function and call rows resolve their
// module context from the innermost scope.
type elixirModuleScope struct {
	name string
	kind string
	line int
}

// elixirFunctionScope carries the enclosing function identity used to attach
// call context and dynamic-dispatch blockers without a separate line index.
type elixirFunctionScope struct {
	name string
	line int
	item map[string]any
}

func newElixirExtractor(
	payload map[string]any,
	source []byte,
	isDependency bool,
	options shared.Options,
) *elixirExtractor {
	return &elixirExtractor{
		payload:      payload,
		source:       source,
		isDependency: isDependency,
		options:      options,
		facts:        newElixirDeadCodeFacts(),
		seenCalls:    make(map[string]struct{}),
	}
}

// extract walks the whole tree once, populating payload buckets in AST order.
func (e *elixirExtractor) extract(root *tree_sitter.Node) {
	e.walk(root, elixirModuleScope{}, nil)
}

// walk descends through named children, dispatching the Elixir definition and
// expression node kinds that produce payload rows. moduleScope is the innermost
// enclosing module; fnScope is the innermost enclosing function.
func (e *elixirExtractor) walk(
	node *tree_sitter.Node,
	moduleScope elixirModuleScope,
	fnScope *elixirFunctionScope,
) {
	if node == nil {
		return
	}

	if node.Kind() == "call" {
		head := elixirCallHead(node, e.source)
		switch {
		case elixirModuleKeyword(head):
			e.handleModuleCall(node, head, moduleScope, fnScope)
			return
		case elixirFunctionKeyword(head):
			e.handleFunctionCall(node, head, moduleScope, fnScope)
			return
		case elixirImportKeyword(head):
			e.handleImportCall(node, head, moduleScope)
			return
		default:
			e.handleExpressionCall(node, moduleScope, fnScope)
		}
	} else if node.Kind() == "unary_operator" {
		if e.handleAttribute(node, moduleScope) {
			return
		}
	}

	for _, child := range elixirNamedChildren(node) {
		child := child
		e.walk(&child, moduleScope, fnScope)
	}
}

// handleModuleCall records a defmodule/defprotocol/defimpl row and recurses into
// the module body with the new module scope.
func (e *elixirExtractor) handleModuleCall(
	node *tree_sitter.Node,
	keyword string,
	moduleScope elixirModuleScope,
	fnScope *elixirFunctionScope,
) {
	name := elixirDefinitionName(node, e.source)
	if name == "" {
		// A module head with no resolvable name was a definition line the prior
		// scanner skipped; walk only the block body, never the signature.
		e.walkBlockBody(node, moduleScope, fnScope)
		return
	}
	startLine := shared.NodeLine(node)
	item := map[string]any{
		"name":          name,
		"line_number":   startLine,
		"end_line":      elixirCallEndLine(node),
		"lang":          "elixir",
		"is_dependency": e.isDependency,
		"type":          keyword,
		"module_kind":   moduleKind(keyword),
	}
	if keyword == "defimpl" {
		item["protocol"] = name
		if implementedFor := elixirDefImplTarget(node, e.source); implementedFor != "" {
			item["implemented_for"] = implementedFor
		}
	}
	if e.options.IndexSource {
		item["source"] = elixirNodeSource(node, e.source)
	}
	if keyword == "defprotocol" {
		shared.AppendBucket(e.payload, "protocols", item)
	} else {
		shared.AppendBucket(e.payload, "modules", item)
	}
	recordElixirModule(e.facts, name)

	nextScope := elixirModuleScope{name: name, kind: moduleKind(keyword), line: startLine}
	e.walkChildren(node, nextScope, fnScope)
}

// handleFunctionCall records a def/defp/defmacro/... row and recurses into the
// function body with the new function scope so nested calls attach context.
func (e *elixirExtractor) handleFunctionCall(
	node *tree_sitter.Node,
	keyword string,
	moduleScope elixirModuleScope,
	fnScope *elixirFunctionScope,
) {
	target := elixirDefinitionTargetCall(node)
	if target == nil {
		// A def head with no resolvable target was a definition line the prior
		// scanner skipped; walk only the block body, never the signature.
		e.walkBlockBody(node, moduleScope, fnScope)
		return
	}
	name := elixirDefinitionTargetName(target, e.source)
	if name == "" {
		e.walkBlockBody(node, moduleScope, fnScope)
		return
	}

	startLine := shared.NodeLine(node)
	endLine := elixirCallEndLine(node)
	args := elixirCallArgumentTexts(target, e.source)
	decorators := e.decoratorsBefore(node)

	item := map[string]any{
		"name":                  name,
		"line_number":           startLine,
		"end_line":              endLine,
		"args":                  args,
		"lang":                  "elixir",
		"is_dependency":         e.isDependency,
		"visibility":            "public",
		"type":                  keyword,
		"decorators":            []string{},
		"semantic_kind":         functionSemanticKind(keyword),
		"cyclomatic_complexity": elixirCyclomaticComplexity(node, e.source),
	}
	if strings.HasSuffix(keyword, "p") {
		item["visibility"] = "private"
	}
	if len(decorators) > 0 {
		item["decorators"] = decorators
	}
	if e.options.IndexSource {
		item["source"] = elixirNodeSource(node, e.source)
		if docstring := e.docstringBefore(node); docstring != "" {
			item["docstring"] = docstring
		}
	}
	if moduleScope.name != "" {
		item["context"] = []any{moduleScope.name, "module", moduleScope.line}
		item["context_type"] = "module"
		item["class_context"] = moduleScope.name
	}

	pendingImpl := elixirHasImplDecorator(decorators)
	rootKinds := elixirFunctionDeadCodeRootKinds(
		keyword,
		name,
		args,
		moduleScope.name,
		moduleScope.kind,
		pendingImpl,
		e.facts,
	)
	if len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}

	e.markFunctionDynamicDispatch(node, item)
	shared.AppendBucket(e.payload, "functions", item)

	e.functions = append(e.functions, elixirFunctionSpan{
		keyword:    keyword,
		name:       name,
		moduleName: moduleScope.name,
		args:       args,
		startLine:  startLine,
		endLine:    endLine,
	})

	if keyword == "defguard" || keyword == "defguardp" {
		e.appendGuardCalls(node, target, name, moduleScope, startLine)
	}

	nextFn := &elixirFunctionScope{name: name, line: startLine, item: item}
	e.walkBlockBody(node, moduleScope, nextFn)
}

// walkChildren visits the named children of node with the given scopes.
func (e *elixirExtractor) walkChildren(
	node *tree_sitter.Node,
	moduleScope elixirModuleScope,
	fnScope *elixirFunctionScope,
) {
	for _, child := range elixirNamedChildren(node) {
		child := child
		e.walk(&child, moduleScope, fnScope)
	}
}

// walkBlockBody recurses into the `do_block` body of a definition and nothing
// else. Only the block body produces calls. A one-line `def ..., do: expr`
// definition keeps its body inside the signature `arguments` node, which the
// prior line scanner treated as a definition line and never mined for calls;
// that behavior is preserved here so one-line bodies do not emit call rows.
// Dynamic dispatch in one-line bodies is still detected separately for exactness
// blockers. The same body-only walk is the safe fallback for a malformed module
// or function head whose name could not be resolved.
func (e *elixirExtractor) walkBlockBody(
	node *tree_sitter.Node,
	moduleScope elixirModuleScope,
	fnScope *elixirFunctionScope,
) {
	for _, child := range elixirNamedChildren(node) {
		child := child
		if child.Kind() == "do_block" {
			e.walk(&child, moduleScope, fnScope)
		}
	}
}
