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
func securityAlertRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := alerttools.Route(toolName, routecontract.Arguments(args))
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
