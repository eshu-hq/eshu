// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualizationtools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for the visualization-packet tool
// without executing it. It reports handled only for tools owned by this
// package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "derive_visualization_packet":
		return routecontract.Request{Method: "POST", Path: "/api/v0/visualizations/derive", Body: map[string]any{
			"view":            args.String("view"),
			"source_response": args["source_response"],
			"source_truth":    args["source_truth"],
		}}, true
	default:
		return routecontract.Request{}, false
	}
}
