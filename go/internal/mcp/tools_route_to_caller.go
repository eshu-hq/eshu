// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func routeToCallerTool() ToolDefinition {
	return ToolDefinition{
		Name:        "trace_route_callers",
		Description: "Resolve an exact framework route handler from HANDLES_ROUTE truth, then return bounded CALLS callers/callees and impacted workloads/repositories. Dynamic or unsupported routes are reported without guessing handlers.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Canonical repository identifier to scope route resolution",
				},
				"service_id": map[string]any{
					"type":        "string",
					"description": "Optional exact service/workload identifier to scope route resolution when repo_id is not supplied",
				},
				"service_name": map[string]any{
					"type":        "string",
					"description": "Optional exact service/workload name to scope route resolution when repo_id is not supplied",
				},
				"method": map[string]any{
					"type":        "string",
					"description": "HTTP method to match exactly against HANDLES_ROUTE.http_method; omit only when the caller intentionally wants method ambiguity surfaced",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Exact route path as projected on the Endpoint node",
				},
				"max_depth": map[string]any{
					"type":        "integer",
					"description": "Maximum CALLS traversal depth",
					"default":     2,
					"minimum":     1,
					"maximum":     5,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum combined caller/callee rows to return",
					"default":     25,
					"minimum":     1,
					"maximum":     100,
				},
			},
			"required": []string{"path"},
		},
	}
}
