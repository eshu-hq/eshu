// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	visualizationtools "github.com/eshu-hq/eshu/go/internal/mcp/visualization"
)

// visualizationRoute adapts the child package's visualization request into
// the root dispatcher's transport route.
func visualizationRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	request, handled := visualizationtools.Route(toolName, routecontract.Arguments(args))
	if !handled {
		return nil, false
	}
	return &routecontract.Request{
		Method: request.Method,
		Path:   request.Path,
		Body:   request.Body,
		Query:  request.Query,
	}, true
}
