// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	alerttools "github.com/eshu-hq/eshu/go/internal/mcp/alerts"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// securityAlertRoute adapts the child package's security-alert
// reconciliation request selection into the root dispatcher's transport
// route.
func securityAlertRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(alerttools.Route(toolName, routecontract.Arguments(args)))
}
