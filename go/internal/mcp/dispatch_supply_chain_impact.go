// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	supplychainimpacttools "github.com/eshu-hq/eshu/go/internal/mcp/supply/chain/impact"
)

// supplyChainImpactRoute adapts the child package's supply-chain-impact
// request selection into the root dispatcher's transport route.
func supplyChainImpactRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(supplychainimpacttools.Route(toolName, routecontract.Arguments(args)))
}
