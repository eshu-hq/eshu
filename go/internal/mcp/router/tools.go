// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"

const (
	// FindToolName is the MCP name of the tool-discovery tool.
	FindToolName = "find_tool"
	// CallToolName is the MCP name of the tool-invocation tool.
	CallToolName = "call_tool"
)

const (
	// DefaultLimit is the find_tool candidate count used when limit is omitted.
	DefaultLimit = 5
	// MaxLimit is the largest find_tool candidate count a caller may request.
	MaxLimit = 10
)

// Tools returns fresh MCP definitions for find_tool then call_tool. They are
// not part of mcp.ReadOnlyTools: the mcp root intercepts them before
// dispatchTool.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        FindToolName,
			Description: "Find the Eshu tools that fit a task. Describe what you want to do in intent, optionally add known entity ids in anchors, and get back ranked candidates with a reason, cost, input schema, and example arguments. Then run one with call_tool.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent": map[string]any{
						"type":        "string",
						"description": "What you want to learn or do, in plain words.",
					},
					"anchors": map[string]any{
						"type":        "object",
						"description": "Optional identifiers you already know. They narrow the candidates to tools that accept them.",
						"properties": map[string]any{
							"repo_id":     map[string]any{"type": "string", "description": "Repository id."},
							"service_id":  map[string]any{"type": "string", "description": "Service id."},
							"workload_id": map[string]any{"type": "string", "description": "Workload id."},
							"environment": map[string]any{"type": "string", "description": "Environment name."},
						},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of candidates to return (1-10).",
						"default":     DefaultLimit,
						"minimum":     1,
						"maximum":     MaxLimit,
					},
				},
				"required": []string{"intent"},
			},
		},
		{
			Name:        CallToolName,
			Description: "Run one Eshu tool by name with its arguments, as found by find_tool. Arguments are checked against the tool's input schema first. The response is the same as calling the tool directly. It cannot call find_tool, call_tool, or ask.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Registered tool name from a find_tool candidate.",
					},
					"arguments": map[string]any{
						"type":        "object",
						"description": "Arguments for the named tool, matching its input schema.",
					},
				},
				"required": []string{"name", "arguments"},
			},
		},
	}
}
