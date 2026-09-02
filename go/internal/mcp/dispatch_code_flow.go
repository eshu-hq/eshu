// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	codeflowtools "github.com/eshu-hq/eshu/go/internal/mcp/codeflow"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// codeFlowRoute adapts the child package's code-flow request selection into
// the root dispatcher's transport route. It keeps the same delegation
// position in resolveRoute the family's own selector answered from before
// the extraction.
func codeFlowRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := codeflowtools.Route(toolName, routecontract.Arguments(args))
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
