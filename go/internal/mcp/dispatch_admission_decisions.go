// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	admissiondecisionstools "github.com/eshu-hq/eshu/go/internal/mcp/admission/decisions"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// admissionDecisionsRoute adapts the child package's admission-decisions
// request selection into the root dispatcher's transport route.
func admissionDecisionsRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	request, handled := admissiondecisionstools.Route(toolName, routecontract.Arguments(args))
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
