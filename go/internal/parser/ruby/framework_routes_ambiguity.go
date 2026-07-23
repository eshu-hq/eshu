// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// rubyRailsRouteSetMethods is the set of Rails::RouteSet methods that each
// open a full route-registration block: `draw` (the normal application
// routes.rb entrypoint) plus `append` and `prepend` (real, documented Rails
// APIs engines and gems use to insert routes after/before the main set). All
// three share the identical `Rails.application.routes` receiver shape, so a
// controller action routed ONLY inside an `.append`/`.prepend` block is just
// as real as one routed inside `.draw` -- treating only `.draw` as
// route-registration context (the pre-P1-fix behavior) let such an action
// bypass BOTH exact-route capture (rubyResolveRouteContext in
// framework_routes.go would never resolve "rails" for it) AND the ambiguity
// scan below (only triggered for a call this set matches), silently
// downgrading a live controller to route_unreachable in an otherwise
// exact-only repo.
var rubyRailsRouteSetMethods = map[string]struct{}{
	"draw":    {},
	"append":  {},
	"prepend": {},
}

// isRailsRoutesDraw reports whether node is a Rails route-set registration
// call (Rails.application.routes.draw/append/prepend). Shared by
// rubyResolveRouteContext (framework_routes.go, exact-route capture) and
// rubyScanRailsDrawBlockForAmbiguity below (ambiguity detection) -- both
// consumers must agree on what counts as route-registration context, or one
// could recognize a block the other misses.
func (s *rubySyntax) isRailsRoutesDraw(node *tree_sitter.Node) bool {
	method := node.ChildByFieldName("method")
	if _, ok := rubyRailsRouteSetMethods[s.text(method)]; !ok {
		return false
	}
	receiver := node.ChildByFieldName("receiver")
	return s.receiverName(receiver) == "Rails.application.routes"
}

// appendRubyRailsRouteAmbiguity stamps has_unmodeled_routes=true onto the
// "rails" framework_semantics section, creating an (otherwise empty) section
// when the file registered zero resolvable route_entries. The #5494 reducer
// join must see this signal for every file containing any dynamic or
// unresolved Rails route registration, not only files that ALSO produced
// exact routes elsewhere.
func appendRubyRailsRouteAmbiguity(semantics map[string]any) {
	rails, ok := semantics["rails"].(map[string]any)
	if !ok {
		rails = map[string]any{}
		semantics["rails"] = rails
		semantics["frameworks"] = append(semantics["frameworks"].([]string), "rails")
	}
	rails["has_unmodeled_routes"] = true
}

// rubyScanRailsDrawBlockForAmbiguity reports whether drawCall -- a "call" node
// the caller has already confirmed is a Rails route-set registration call
// (Rails.application.routes.draw/append/prepend, see rubyRailsRouteSetMethods)
// -- has a block (do...end or one-line { ... }) containing any call the
// parser cannot fully model into an exact (path, controller#action) route
// entry.
//
// This is a FAIL-SAFE, default-to-ambiguous design, not an allow-list of known
// problem shapes. An earlier version of this detector special-cased only
// resources/resource DSL macros and unresolved `to:` targets; that missed
// `root`, `match`, gem-provided route macros (devise_for, and any other gem's
// arbitrary DSL method), controller:/action: keyword-pair routes, bare or
// interpolated paths, and non-string `to:` targets -- every one of those
// silently fell through BOTH the exact-route capture and the old ambiguity
// check, so a controller routed ONLY through one of them could be
// misclassified as genuinely dead (the exact false-positive #5494 exists to
// prevent). Enumerating "known problem shapes" is unbounded (any Rails engine
// or gem can add its own routing DSL method), so the only safe rule is the
// inverse: every call inside the draw block is ambiguous UNLESS it resolves
// into an exact route entry (railsExactRouteEntry). This is deliberately
// over-inclusive -- for example a stray `Rails.env.production?` guard call
// inside the block also sets the flag -- but over-inclusive ambiguity only
// ever biases the #5494 reducer join toward KEEP, never toward a wrong
// downgrade.
//
// It walks only drawCall's block child (`Rails.application.routes.draw
// do ... end`'s tree-sitter-ruby shape wraps the receiver chain
// "Rails.application.routes" as nested "call" nodes SIBLING to the block,
// not inside it -- scanning the whole drawCall subtree would misclassify that
// receiver chain's own "call" nodes as unmodeled routes). A drawCall with no
// block (a bare `Rails.application.routes.draw` reference, never valid
// Rails, but handled defensively) is never ambiguous: there is no block body
// to contain an unmodeled route. Walking only the block bounds the cost to
// the size of the routes.draw block itself, not to climbing from every
// receiverless call in the whole file, which is what applying this check at
// the generic call-node level would cost on a large non-routes file.
func (s *rubySyntax) rubyScanRailsDrawBlockForAmbiguity(drawCall *tree_sitter.Node) bool {
	block := rubyDoBlockChild(drawCall)
	if block == nil {
		return false
	}
	ambiguous := false
	shared.WalkNamed(block, func(node *tree_sitter.Node) {
		if node.Kind() != "call" {
			return
		}
		if _, ok := s.railsExactRouteEntry(node); !ok {
			ambiguous = true
		}
	})
	return ambiguous
}

// rubyDoBlockChild returns node's direct block child -- either a "do_block"
// (`draw do ... end`) or a one-line "{ ... }" "block" (`draw { ... }`,
// tree-sitter-ruby's curly-brace block shape) -- or nil if node has none.
// RuboCop's default style enforces do...end for multi-line blocks, so the
// curly-brace form is rare for a routes.draw call, but a fail-safe scan
// cannot assume a style guide is followed.
func rubyDoBlockChild(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return nil
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && (child.Kind() == "do_block" || child.Kind() == "block") {
			return child
		}
		if !cursor.GotoNextSibling() {
			return nil
		}
	}
}
