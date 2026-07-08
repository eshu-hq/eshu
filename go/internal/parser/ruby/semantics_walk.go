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
// names) plus every framework route buildRubyFrameworkSemantics needs. This
// single shared.WalkNamed pass replaces what were four independent full-tree
// walks per Ruby file: three dead-code shared.WalkNamed passes
// (rubyRailsCallbackMethodNames, rubyLiteralMethodReferenceNames,
// rubyScriptEntrypointCallNames) and a bespoke recursive route walk
// (collectRubyRoutes). The three dead-code checks are pure node-local
// predicates with no interaction, so they run unconditionally for every
// "call" node. Route resolution instead climbs from a call node to its
// nearest context-changing ancestor (rubyResolveRouteContext) rather than
// having context threaded down during descent, which is what let the route
// walk fold into this same flat pass without reordering or altering any of
// the four original analyses.
func rubyCollectSemantics(syntax *rubySyntax) (rubyDeadCodeNames, map[string][]rubyRoute) {
	names := rubyDeadCodeNames{
		railsCallback:    make(map[string]struct{}),
		methodReference:  make(map[string]struct{}),
		scriptEntrypoint: make(map[string]struct{}),
	}
	routesByFramework := make(map[string][]rubyRoute)
	topLevelSinatra := rubyImportsSinatra(syntax.imports)

	shared.WalkNamed(syntax.root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call":
			rubyCollectRailsCallbackNames(syntax, node, names.railsCallback)
			rubyCollectMethodReferenceNames(syntax, node, names.methodReference)
			if framework, route, ok := syntax.rubyCollectRouteCandidate(node, topLevelSinatra); ok {
				routesByFramework[framework] = append(routesByFramework[framework], route)
			}
		case "if", "unless":
			rubyCollectScriptEntrypointNames(syntax, node, names.scriptEntrypoint)
		}
	})
	return names, routesByFramework
}
