// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	packageregistrytools "github.com/eshu-hq/eshu/go/internal/mcp/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// packageRegistryRoute adapts the child package's package-registry request into
// the root dispatcher's transport route.
func packageRegistryRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := packageregistrytools.Route(toolName, routecontract.Arguments(args))
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
