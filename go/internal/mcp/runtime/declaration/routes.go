// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimedeclarationtools

import "github.com/eshu-hq/eshu/go/internal/mcp/contract/route"

// Route selects the internal HTTP request for the runtime declaration tool
// without executing it. It reports handled only for list_runtime_version_declarations;
// family membership is an explicit name, never a prefix match.
//
// The body carries every argument the caller sent, as sent. The handler
// decodes strictly and rejects an unknown field or a wrong-typed value with a
// 400 that names it, so a misspelled filter (majorbelow) or a string where an
// integer belongs (major_below "20") fails loudly. Selecting keys here would
// drop them silently, and a dropped filter answers with every declaration,
// which reads as a clean fleet.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	if toolName != ToolName {
		return routecontract.Request{}, false
	}
	body := make(map[string]any, len(args))
	for key, value := range args {
		body[key] = value
	}
	return routecontract.Request{Method: "POST", Path: "/api/v0/runtime-declarations/query", Body: body}, true
}
