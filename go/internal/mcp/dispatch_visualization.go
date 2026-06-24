// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func visualizationRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "derive_visualization_packet":
		return &route{method: "POST", path: "/api/v0/visualizations/derive", body: map[string]any{
			"view":            str(args, "view"),
			"source_response": args["source_response"],
			"source_truth":    args["source_truth"],
		}}, true
	default:
		return nil, false
	}
}
