// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	secretsiamtools "github.com/eshu-hq/eshu/go/internal/mcp/secretsiam"
)

// secretsIAMRoute adapts the child package's secrets/IAM posture request into
// the root dispatcher's transport route.
func secretsIAMRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := secretsiamtools.Route(toolName, routecontract.Arguments(args))
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
