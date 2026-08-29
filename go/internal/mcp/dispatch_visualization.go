// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	visualizationtools "github.com/eshu-hq/eshu/go/internal/mcp/visualization"
)

// visualizationRoute adapts the child package's visualization request into
// the root dispatcher's transport route.
func visualizationRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := visualizationtools.Route(toolName, routecontract.Arguments(args))
	if !handled {
		return nil, false
	}
	return &route{
		method: request.Method,
		path:   request.Path,
		body:   request.Body,
		query:  request.Query,
	}, true
}
