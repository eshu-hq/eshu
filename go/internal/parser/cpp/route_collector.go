// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type cppRoute struct {
	method  string
	path    string
	handler string
}

// cppRouteCollector accumulates Crow, Drogon, and Pistache route candidates
// across call_expression nodes visited during Parse's main shared.WalkNamed
// pass. Before the walk-collapse fix (issue #4841, epic #4831),
// buildCPPFrameworkSemantics ran its own dedicated full-tree walk over
// "call_expression" nodes; Parse's main walk already visits every
// call_expression node (for appendCall), and the framework-route check reads
// only the node's own text with no dependency on any other collector's
// output, so collect now runs from that same case instead of a second
// traversal.
type cppRouteCollector struct {
	routesByFramework map[string][]cppRoute
	seen              map[string]struct{}
}

func newCPPRouteCollector() *cppRouteCollector {
	return &cppRouteCollector{
		routesByFramework: map[string][]cppRoute{},
		seen:              map[string]struct{}{},
	}
}

// collect records Crow/Drogon/Pistache route evidence for one call_expression
// node. Called from Parse's main shared.WalkNamed pass.
func (c *cppRouteCollector) collect(node *tree_sitter.Node, source []byte) {
	text := strings.TrimSpace(shared.NodeText(node, source))
	for framework, routes := range map[string][]cppRoute{
		"crow":     cppCrowRoutes(text),
		"drogon":   cppDrogonRoutes(text),
		"pistache": cppPistacheRoutes(text),
	} {
		for _, route := range routes {
			key := framework + "\x00" + route.method + "\x00" + route.path + "\x00" + route.handler
			if _, ok := c.seen[key]; ok {
				continue
			}
			c.seen[key] = struct{}{}
			c.routesByFramework[framework] = append(c.routesByFramework[framework], route)
		}
	}
}

// finalize builds the framework_semantics payload value from the routes
// collected across the main walk.
func (c *cppRouteCollector) finalize() map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	appendCPPRouteFramework(semantics, "crow", c.routesByFramework["crow"])
	appendCPPRouteFramework(semantics, "drogon", c.routesByFramework["drogon"])
	appendCPPRouteFramework(semantics, "pistache", c.routesByFramework["pistache"])
	return semantics
}
