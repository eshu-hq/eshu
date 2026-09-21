// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	cicdtools "github.com/eshu-hq/eshu/go/internal/mcp/cicd"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// cicdRoute adapts the child package's CI/CD run-correlation request into the
// root dispatcher's transport route.
func cicdRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	request, handled := cicdtools.Route(toolName, routecontract.Arguments(args))
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
