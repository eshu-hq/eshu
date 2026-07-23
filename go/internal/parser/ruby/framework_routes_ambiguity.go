// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// rubyRailsDynamicRouteMethods is the set of Rails routing DSL macro methods
// that each register more than one route implicitly (RESTful resource
// routes, nested member/collection routes, "only"/"except" scoping, and
// similar). The parser deliberately does not expand these into concrete
// controller#action mappings -- doing so exactly would require modeling
// Rails' full routing DSL (nesting, module/scope prefixes, custom member
// routes, concerns) -- so the mere PRESENCE of one of these calls inside a
// Rails.application.routes.draw block is treated as a positive ambiguity
// signal instead: a repo containing one can never have a controller action
// safely route-downgraded from route_entries alone, because route_entries has
// no visibility into the routes these macros generate (#5494).
var rubyRailsDynamicRouteMethods = map[string]struct{}{
	"resources": {},
	"resource":  {},
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

// rubyIsDynamicRailsRouteCall reports whether node is a receiverless call to a
// Rails routing DSL macro (resources/resource) resolved to the Rails
// route-draw context. It is checked independently of rubyCollectRouteCandidate:
// resources/resource take a bare symbol argument (":posts"), never a literal
// exact path string, so they never match the HTTP-verb route shape.
func (s *rubySyntax) rubyIsDynamicRailsRouteCall(node *tree_sitter.Node, topLevelSinatra bool) bool {
	if node.ChildByFieldName("receiver") != nil {
		return false
	}
	method := node.ChildByFieldName("method")
	if method == nil {
		return false
	}
	if _, ok := rubyRailsDynamicRouteMethods[s.text(method)]; !ok {
		return false
	}
	return s.rubyResolveRouteContext(node, topLevelSinatra).framework == "rails"
}

// hasRoutePairTo reports whether node carries a `to:` argument pair with a
// non-empty string value, regardless of whether railsRouteHandler could parse
// it into a clean "controller#action" shape. Used by rubyCollectRouteCandidate
// (framework_routes.go) to distinguish an unresolved-but-real route
// registration (#5494 ambiguity) from no route call at all.
func (s *rubySyntax) hasRoutePairTo(node *tree_sitter.Node) bool {
	_, ok := s.routePairStringValue(node, "to")
	return ok
}
