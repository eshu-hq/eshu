// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	secretsiamtools "github.com/eshu-hq/eshu/go/internal/mcp/access/posture"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// secretsIAMRoute adapts the child package's secrets/IAM posture request into
// the root dispatcher's transport route.
func secretsIAMRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(secretsiamtools.Route(toolName, routecontract.Arguments(args)))
}
