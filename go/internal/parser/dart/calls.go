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

// dartCallChain reconstructs the dotted callee chain of one Dart expression as
// its enclosing node's named children are visited in source order, emitting a
// dartCallSite whenever the chain is closed by an argument list.
//
// tree-sitter-dart has no single `invocation_expression` node kind (unlike the
// C# grammar this package mirrors). A call site is instead a primary node (an
// `identifier`/`type_identifier`/`super`) followed by zero or more chained
// `selector` siblings, where a `selector` wrapping an `argument_part` (or, for
// cascades and object-creation expressions, a bare `argument_part`/`arguments`
// sibling) marks the call. Declarations use entirely disjoint node kinds
// (`function_signature`, `method_signature`, `constructor_signature` and its
// factory/const/redirecting variants, all of which wrap a
// `formal_parameter_list`, never an `arguments`/`argument_part` node), so a
// declaration name identifier starts a chain that is discarded, never emitted.
//
// One dartCallChain is a frame-local of a single dartSyntaxIndex.collect
// invocation (see syntax_index.go): call-site detection is folded into that
// single declaration+call traversal (#5350) rather than run as a separate
// second full tree walk. Each collect frame owns a fresh chain for its node's
// own named children, since nested expressions (call arguments, lambda bodies,
// cascade sections) form their own independent chains when collect recurses.
type dartCallChain struct {
	chain     []string
	chainLine int
	// previousWasPrimary tracks whether the last named child was a primary
	// identifier, so a following bare identifier can be recognized as a
	// dotted-name continuation (see allowDotIdentifierContinuation).
	previousWasPrimary bool
	// allowDotIdentifierContinuation is true only when the enclosing node is
	// an object-creation shape (`new Foo.bar()`, `const Foo.bar()`,
	// `@Foo.bar(...)`), where two adjacent identifiers are a dotted callee
	// separated by an unnamed `.` token. In every other parent (e.g.
	// `var result = compute(a, b);`, where `result` is the declared name and
	// `compute` starts the unrelated initializer) two adjacent identifiers are
	// unrelated and must not be joined into one dotted chain.
	allowDotIdentifierContinuation bool
}

func (c *dartCallChain) emit(sites *[]dartCallSite) {
	if len(c.chain) == 0 {
		return
	}
	*sites = append(*sites, dartCallSite{
		name:     c.chain[len(c.chain)-1],
		fullName: strings.Join(c.chain, "."),
		line:     c.chainLine,
	})
}

func (c *dartCallChain) reset() {
	c.chain = nil
	c.chainLine = 0
	c.previousWasPrimary = false
}

func (c *dartCallChain) extend(name string, line int) {
	c.chain = append(c.chain, name)
	c.chainLine = line
}

// extendOrStart appends name to an in-progress chain, or starts a fresh
// one-element chain if the previous call boundary cleared it (e.g. the
// `.then` in `fetch().then(process)`: the qualifier "fetch" is lost at the
// `fetch()` call boundary, but `.then` must still start its own chain rather
// than being dropped because the chain was empty).
func (c *dartCallChain) extendOrStart(name string, line int) {
	if name == "" {
		c.reset()
		return
	}
	if len(c.chain) > 0 {
		c.extend(name, line)
		return
	}
	c.chain = []string{name}
	c.chainLine = line
}

// observe advances the chain state for one named child and appends a call site
// to sites when the child closes the chain on an argument list. It is the
// per-child call-detection switch, run on each named child before collect
// recurses into it, so emission order matches a pre-order full-tree walk.
func (c *dartCallChain) observe(child *tree_sitter.Node, source []byte, sites *[]dartCallSite) {
	switch child.Kind() {
	case "identifier", "type_identifier":
		text := strings.TrimSpace(shared.NodeText(child, source))
		// A bare identifier immediately after another primary identifier only
		// occurs for the `_dot_identifier` shape used by
		// `new`/`const`/annotation object-creation expressions
		// (`const Foo.bar()`), where the "." is an unnamed token. Any other
		// adjacency between two primaries cannot occur in valid Dart, so
		// treating this as a dotted continuation is safe.
		if c.allowDotIdentifierContinuation && c.previousWasPrimary && len(c.chain) > 0 {
			c.extend(text, shared.NodeLine(child))
		} else {
			c.reset()
			if text != "" {
				c.extend(text, shared.NodeLine(child))
			}
		}
		c.previousWasPrimary = true
	case "super":
		c.reset()
		c.extend("super", shared.NodeLine(child))
		c.previousWasPrimary = true
	case "cascade_selector":
		c.reset()
		if name := dartSelectorIdentifier(child, source); name != "" {
			c.extend(name, shared.NodeLine(child))
		}
	case "unconditional_assignable_selector", "conditional_assignable_selector":
		// Direct sibling shape used by `super.m()`; the general `o.m()` shape
		// wraps the same node kinds inside a `selector` (handled below).
		c.extendOrStart(dartSelectorIdentifier(child, source), shared.NodeLine(child))
	case "selector":
		inner := dartFirstNamedChild(child)
		switch {
		case inner == nil:
			c.reset()
		case inner.Kind() == "unconditional_assignable_selector" || inner.Kind() == "conditional_assignable_selector":
			c.extendOrStart(dartSelectorIdentifier(inner, source), shared.NodeLine(inner))
		case inner.Kind() == "argument_part":
			c.emit(sites)
			c.reset()
		case inner.Kind() == "type_arguments":
			// Generic-args selector (`f<int>()`): keep the chain: the
			// call-completing `argument_part` selector still follows.
		default:
			c.reset()
		}
	case "argument_part", "arguments":
		// Direct (non-`selector`-wrapped) call completion: cascade sections
		// (`o..m()`) and `new`/`const`/annotation object-creation expressions
		// (`new Foo()`, `const Foo()`, `@Foo(...)`).
		c.emit(sites)
		c.reset()
	default:
		c.reset()
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
