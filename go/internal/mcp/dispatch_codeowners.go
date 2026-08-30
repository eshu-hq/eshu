// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	codeownerstools "github.com/eshu-hq/eshu/go/internal/mcp/codeowners"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// codeownersRoute adapts the child package's CODEOWNERS ownership request into
// the root dispatcher's transport route.
func codeownersRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := codeownerstools.Route(toolName, routecontract.Arguments(args))
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
