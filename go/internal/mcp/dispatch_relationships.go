// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	relationshiptools "github.com/eshu-hq/eshu/go/internal/mcp/relationships"
)

// codeRelationshipRoute adapts the child package's code-relationship request
// into the root dispatcher's transport route.
func codeRelationshipRoute(toolName string, args map[string]any) (*routecontract.Request, bool, error) {
	request, handled, err := codeRelationshipRequest(toolName, routecontract.Arguments(args))
	if !handled || err != nil {
		return nil, handled, err
	}
	adapted, handled := adaptChildRoute(request, handled)
	return adapted, handled, nil
}

// codeRelationshipRequest delegates family membership and request selection to
// the child package while the root retains global fanout, adapter, and dispatch
// ownership.
func codeRelationshipRequest(toolName string, args routecontract.Arguments) (routecontract.Request, bool, error) {
	return relationshiptools.CodeRoute(toolName, args)
}
