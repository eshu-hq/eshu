// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	relationshiptools "github.com/eshu-hq/eshu/go/internal/mcp/relationships"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// codeRelationshipRoute adapts the child package's code-relationship request
// into the root dispatcher's transport route.
func codeRelationshipRoute(toolName string, args map[string]any) (*route, bool, error) {
	request, handled, err := codeRelationshipRequest(toolName, routecontract.Arguments(args))
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

// codeRelationshipRequest delegates family membership and request selection to
// the child package while the root retains global fanout, adapter, and dispatch
// ownership.
func codeRelationshipRequest(toolName string, args routecontract.Arguments) (routecontract.Request, bool, error) {
	return relationshiptools.CodeRoute(toolName, args)
}
