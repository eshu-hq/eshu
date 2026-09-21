// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	infrasearchtools "github.com/eshu-hq/eshu/go/internal/mcp/infra/search"
)

// infraResourceSearchRoute adapts the child package's infrastructure-search
// request selection into the root dispatcher's transport route.
func infraResourceSearchRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	request, handled := infrasearchtools.Route(toolName, routecontract.Arguments(args))
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
