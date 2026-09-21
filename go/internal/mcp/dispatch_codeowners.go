// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	codeownerstools "github.com/eshu-hq/eshu/go/internal/mcp/code/owners"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// codeownersRoute adapts the child package's CODEOWNERS ownership request into
// the root dispatcher's transport route.
func codeownersRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(codeownerstools.Route(toolName, routecontract.Arguments(args)))
}
