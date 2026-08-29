// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	asktools "github.com/eshu-hq/eshu/go/internal/mcp/ask"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// askRoute maps the "ask" tool to POST /api/v0/ask.
//
// The endpoint is default-off: when ESHU_ASK_ENABLED is unset or the
// agent_reasoning provider profile is missing, the handler returns
// 503 with state "unavailable" rather than running the engine. The
// MCP dispatch surface treats that as a non-error envelope response so
// callers see a clean tool result rather than a transport error.
func askRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := asktools.Route(toolName, routecontract.Arguments(args))
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
