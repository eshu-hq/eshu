// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	asktools "github.com/eshu-hq/eshu/go/internal/mcp/ask"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// askRoute maps the "ask" tool to POST /api/v0/ask.
//
// The endpoint is default-off: when ESHU_ASK_ENABLED is unset or the
// agent_reasoning provider profile is missing, the handler returns
// 503 with state "unavailable" rather than running the engine. The
// MCP dispatch surface treats that as a non-error envelope response so
// callers see a clean tool result rather than a transport error.
func askRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	request, handled := asktools.Route(toolName, routecontract.Arguments(args))
	if !handled {
		return nil, false
	}
	return &routecontract.Request{
		Method: request.Method,
		Path:   request.Path,
		Body:   request.Body,
		Query:  request.Query,
	}, true
}
