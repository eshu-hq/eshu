// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	relationshiptools "github.com/eshu-hq/eshu/go/internal/mcp/relationships"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// relationshipEdgesRoute adapts the child package's relationship-edge request
// into the root dispatcher's transport route. The child validates source_tool
// against the canonical vocabulary before forwarding; an empty value leaves the
// filter unset.
func relationshipEdgesRoute(toolName string, args map[string]any) (*route, bool, error) {
	request, handled, err := relationshiptools.EdgeRoute(toolName, routecontract.Arguments(args))
	if !handled || err != nil {
		return nil, handled, err
	}
	return &route{
		method: request.Method,
		path:   request.Path,
		body:   request.Body,
		query:  request.Query,
	}, true, nil
}
