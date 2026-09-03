// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	servicecontexttools "github.com/eshu-hq/eshu/go/internal/mcp/servicecontext"
)

// serviceContextRoute adapts the child package's service-context request
// selection into the root dispatcher's transport route, propagating the
// child's selector-validation error exactly as the pre-extraction switch
// arms did: get_service_context and get_service_story returned a Go error
// immediately, before ever building an HTTP request, when the caller
// supplied no usable selector. This mirrors relationshipEdgesRoute in
// dispatch_relationship_edges.go, the other adapter that must forward a
// handled=true, err!=nil result rather than only checking handled.
func serviceContextRoute(toolName string, args map[string]any) (*route, bool, error) {
	request, handled, err := servicecontexttools.Route(toolName, routecontract.Arguments(args))
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
