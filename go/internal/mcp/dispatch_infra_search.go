// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	infrasearchtools "github.com/eshu-hq/eshu/go/internal/mcp/infrasearch"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// infraResourceSearchRoute adapts the child package's infrastructure-search
// request selection into the root dispatcher's transport route.
func infraResourceSearchRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := infrasearchtools.Route(toolName, routecontract.Arguments(args))
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
