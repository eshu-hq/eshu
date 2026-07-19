// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// dartCallSite is one AST-derived call expression: a plain/qualified/cascade
// invocation, a named or unnamed constructor invocation, or a `new`/`const`
// object-creation expression.
type dartCallSite struct {
	name     string
	fullName string
	line     int
}

// collectDartCallSites walks the whole Dart syntax tree and returns one
// dartCallSite per call expression found.
//
// tree-sitter-dart has no single `invocation_expression` node kind (unlike
// the C# grammar this package mirrors). A call site is instead a primary
// node (an `identifier`/`type_identifier`/`super`) followed by zero or more
// chained `selector` siblings, where a `selector` wrapping an
// `argument_part` (or, for cascades and object-creation expressions, a bare
// `argument_part`/`arguments` sibling) marks the call. Declarations use
// entirely disjoint node kinds (`function_signature`, `method_signature`,
// `constructor_signature` and its factory/const/redirecting variants, all of
// which wrap a `formal_parameter_list`, never an `arguments`/`argument_part`
// node), so a declaration can never be misclassified as a call here.
func collectDartCallSites(root *tree_sitter.Node, source []byte) []dartCallSite {
	var sites []dartCallSite
	walkDartCallSites(root, source, &sites)
	return sites
}

// walkDartCallSites scans node's own named children in source order,
// reconstructing the dotted callee chain (identifier / `.name` selectors /
// cascade selectors) and emitting a call whenever the chain is closed by an
// argument list. It then recurses into every child with a fresh local chain,
// since nested expressions (call arguments, lambda bodies, cascade sections)
// form their own independent chains.
func walkDartCallSites(node *tree_sitter.Node, source []byte, sites *[]dartCallSite) {
	if node == nil {
		return
	}

	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()

	// A bare identifier immediately following another bare identifier is
	// only a dotted-name continuation (the `_dot_identifier` shape used by
	// `new`/`const`/annotation object-creation expressions, e.g.
	// `const Foo.bar()`) when the enclosing node is one of those shapes.
	// In every other parent (e.g. `var result = compute(a, b);`, where
	// `result` is the declared name and `compute` starts the unrelated
	// initializer expression) two adjacent identifiers are unrelated and
	// must not be joined into one dotted chain.
	allowDotIdentifierContinuation := dartIsObjectCreationNode(node)

	var chain []string
	var chainLine int
	previousWasPrimary := false

	emit := func() {
		if len(chain) == 0 {
			return
		}
		*sites = append(*sites, dartCallSite{
			name:     chain[len(chain)-1],
			fullName: strings.Join(chain, "."),
			line:     chainLine,
		})
	}
	reset := func() {
		chain = nil
		chainLine = 0
		previousWasPrimary = false
	}
	extend := func(name string, line int) {
		chain = append(chain, name)
		chainLine = line
	}
	// extendOrStart appends name to an in-progress chain, or starts a fresh
	// one-element chain if the previous call boundary cleared it (e.g. the
	// `.then` in `fetch().then(process)`: the qualifier "fetch" is lost at
	// the `fetch()` call boundary, but `.then` must still start its own
	// chain rather than being dropped because the chain was empty).
	extendOrStart := func(name string, line int) {
		if name == "" {
			reset()
			return
		}
		if len(chain) > 0 {
			extend(name, line)
			return
		}
		chain = []string{name}
		chainLine = line
	}

	for index := range children {
		child := &children[index]
		switch child.Kind() {
		case "identifier", "type_identifier":
			text := strings.TrimSpace(shared.NodeText(child, source))
			// A bare identifier immediately after another primary
			// identifier only occurs for the `_dot_identifier` shape used
			// by `new`/`const`/annotation object-creation expressions
			// (`const Foo.bar()`), where the "." is an unnamed token. Any
			// other adjacency between two primaries cannot occur in valid
			// Dart, so treating this as a dotted continuation is safe.
			if allowDotIdentifierContinuation && previousWasPrimary && len(chain) > 0 {
				extend(text, shared.NodeLine(child))
			} else {
				reset()
				if text != "" {
					extend(text, shared.NodeLine(child))
				}
			}
			previousWasPrimary = true
		case "super":
			reset()
			extend("super", shared.NodeLine(child))
			previousWasPrimary = true
		case "cascade_selector":
			reset()
			if name := dartSelectorIdentifier(child, source); name != "" {
				extend(name, shared.NodeLine(child))
			}
		case "unconditional_assignable_selector", "conditional_assignable_selector":
			// Direct sibling shape used by `super.m()`; the general
			// `o.m()` shape wraps the same node kinds inside a `selector`
			// (handled below).
			extendOrStart(dartSelectorIdentifier(child, source), shared.NodeLine(child))
		case "selector":
			inner := dartFirstNamedChild(child)
			switch {
			case inner == nil:
				reset()
			case inner.Kind() == "unconditional_assignable_selector" || inner.Kind() == "conditional_assignable_selector":
				extendOrStart(dartSelectorIdentifier(inner, source), shared.NodeLine(inner))
			case inner.Kind() == "argument_part":
				emit()
				reset()
			case inner.Kind() == "type_arguments":
				// Generic-args selector (`f<int>()`): keep the chain: the
				// call-completing `argument_part` selector still follows.
			default:
				reset()
			}
		case "argument_part", "arguments":
			// Direct (non-`selector`-wrapped) call completion: cascade
			// sections (`o..m()`) and `new`/`const`/annotation
			// object-creation expressions (`new Foo()`, `const Foo()`,
			// `@Foo(...)`).
			emit()
			reset()
		default:
			reset()
		}

		walkDartCallSites(child, source, sites)
	}
}

// dartSelectorIdentifier returns the identifier named by an
// unconditional_assignable_selector, conditional_assignable_selector, or
// cascade_selector node (`.name` / `?.name` / the cascade `..name` target).
// It returns "" for an index selector (`[i]`), which names no callee.
func dartSelectorIdentifier(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "identifier" {
			return strings.TrimSpace(shared.NodeText(&child, source))
		}
	}
	return ""
}

// dartIsObjectCreationNode reports whether node is one of the grammar shapes
// that spells a callee as adjacent `type_identifier`/`identifier` siblings
// separated by an unnamed `.` token instead of a `selector`-wrapped
// assignable_selector: `new Foo.bar()`, `const Foo.bar()`, and
// `@Foo.bar(...)` annotations.
func dartIsObjectCreationNode(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "new_expression", "const_object_expression", "annotation", "constructor_invocation":
		return true
	default:
		return false
	}
}

// dartFirstNamedChild returns node's first named child, or nil if it has
// none (for example a `selector` wrapping only the unnamed `!` token).
func dartFirstNamedChild(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	if len(children) == 0 {
		return nil
	}
	return &children[0]
}

// appendUniqueDartCall records one function_calls row, deduplicating by
// fullName (the qualified callee) so repeat calls with the same receiver and
// method collapse to one row, matching the legacy row-volume behavior, while
// distinct receivers with the same method name (`a.foo()` vs `b.foo()`) both
// survive.
func appendUniqueDartCall(payload map[string]any, seen map[string]struct{}, name string, fullName string, lineNumber int) {
	if strings.TrimSpace(name) == "" {
		return
	}
	if _, ok := seen[fullName]; ok {
		return
	}
	seen[fullName] = struct{}{}
	shared.AppendBucket(payload, "function_calls", map[string]any{
		"name":        name,
		"full_name":   fullName,
		"line_number": lineNumber,
		"lang":        "dart",
	})
}
