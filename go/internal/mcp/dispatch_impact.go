// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	impacttools "github.com/eshu-hq/eshu/go/internal/mcp/impact"
)

// impactRoute adapts the child package's impact-analysis request selection
// into the root dispatcher's transport route. It is consulted from
// resolveRoute's default case, the same point in the chain the family's own
// switch occupied before the extraction.
func impactRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(impacttools.Route(toolName, routecontract.Arguments(args)))
}
