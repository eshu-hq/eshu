// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	packageregistrytools "github.com/eshu-hq/eshu/go/internal/mcp/package/registry"
)

// packageRegistryRoute adapts the child package's package-registry request into
// the root dispatcher's transport route.
func packageRegistryRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(packageregistrytools.Route(toolName, routecontract.Arguments(args)))
}
