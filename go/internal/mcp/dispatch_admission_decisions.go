// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	admissiondecisionstools "github.com/eshu-hq/eshu/go/internal/mcp/admissiondecisions"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// admissionDecisionsRoute adapts the child package's admission-decisions
// request selection into the root dispatcher's transport route.
func admissionDecisionsRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := admissiondecisionstools.Route(toolName, routecontract.Arguments(args))
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
