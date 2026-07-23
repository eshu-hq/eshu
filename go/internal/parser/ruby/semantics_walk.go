// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// rubyCollectSemantics walks syntax's AST exactly once, collecting the three
// independent dead-code name sets annotateRubyDeadCodeRoots needs (Rails
// callback registrations, literal method references, script-entrypoint call
// names), every framework route buildRubyFrameworkSemantics needs, and the
// #5494 railsRouteAmbiguous signal. This single shared.WalkNamed pass replaces
// what were four independent full-tree walks per Ruby file: three dead-code
// shared.WalkNamed passes (rubyRailsCallbackMethodNames,
// rubyLiteralMethodReferenceNames, rubyScriptEntrypointCallNames) and a
// bespoke recursive route walk (collectRubyRoutes). The three dead-code checks
// are pure node-local predicates with no interaction, so they run
// unconditionally for every "call" node. Route resolution instead climbs from
// a call node to its nearest context-changing ancestor
// (rubyResolveRouteContext) rather than having context threaded down during
// descent, which is what let the route walk fold into this same flat pass
// without reordering or altering any of the four original analyses.
//
// railsRouteAmbiguous is true when this file contains a
// Rails.application.routes.draw block with any call the parser cannot resolve
// into an exact "controller#action" route_entries row -- a fail-safe,
// default-to-ambiguous scan (rubyScanRailsDrawBlockForAmbiguity, see
// framework_routes_ambiguity.go), not an allow-list of specific problem
// shapes. It is a file-scoped OR: one unresolved route call taints the whole
// file, matching the #5494 reducer join's repo-wide keep-biased use of the
// signal.
func rubyCollectSemantics(syntax *rubySyntax) (rubyDeadCodeNames, map[string][]rubyRoute, bool) {
	names := rubyDeadCodeNames{
		railsCallback:    make(map[string]struct{}),
		methodReference:  make(map[string]struct{}),
		scriptEntrypoint: make(map[string]struct{}),
	}
	routesByFramework := make(map[string][]rubyRoute)
	topLevelSinatra := rubyImportsSinatra(syntax.imports)
	railsRouteAmbiguous := false

	shared.WalkNamed(syntax.root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call":
			rubyCollectRailsCallbackNames(syntax, node, names.railsCallback)
			rubyCollectMethodReferenceNames(syntax, node, names.methodReference)
			if framework, route, ok := syntax.rubyCollectRouteCandidate(node, topLevelSinatra); ok {
				routesByFramework[framework] = append(routesByFramework[framework], route)
			}
			if syntax.isRailsRoutesDraw(node) && syntax.rubyScanRailsDrawBlockForAmbiguity(node) {
				railsRouteAmbiguous = true
			}
		case "if", "unless":
			rubyCollectScriptEntrypointNames(syntax, node, names.scriptEntrypoint)
		}
	})
	return names, routesByFramework, railsRouteAmbiguous
}
