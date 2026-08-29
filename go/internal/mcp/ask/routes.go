// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package asktools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for the Ask tool without executing
// it. It returns handled=false for tools outside the Ask family.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	if toolName != "ask" {
		return routecontract.Request{}, false
	}
	return routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/ask",
		Body: map[string]any{
			"question": args.String("question"),
			"format":   args.String("format"),
		},
	}, true
}
