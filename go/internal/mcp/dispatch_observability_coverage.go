// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	observabilitycoveragetools "github.com/eshu-hq/eshu/go/internal/mcp/observabilitycoverage"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// observabilityCoverageRoute adapts the child package's observability-coverage
// request into the root dispatcher's transport route.
func observabilityCoverageRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := observabilitycoveragetools.Route(toolName, routecontract.Arguments(args))
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
