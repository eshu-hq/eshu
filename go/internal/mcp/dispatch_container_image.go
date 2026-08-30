// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	containerimagetools "github.com/eshu-hq/eshu/go/internal/mcp/containerimage"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// containerImageRoute adapts the child package's container-image identity
// request into the root dispatcher's transport route.
func containerImageRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := containerimagetools.Route(toolName, routecontract.Arguments(args))
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
