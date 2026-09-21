// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	observabilitycoveragetools "github.com/eshu-hq/eshu/go/internal/mcp/observability/coverage"
)

// observabilityCoverageRoute adapts the child package's observability-coverage
// request into the root dispatcher's transport route.
func observabilityCoverageRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	request, handled := observabilitycoveragetools.Route(toolName, routecontract.Arguments(args))
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
