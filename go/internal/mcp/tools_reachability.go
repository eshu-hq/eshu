// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// reachabilityTools returns the code-to-cloud reachability MCP tools (epic
// #2704). The tools are thin wrappers over the /api/v0 reachability routes.
func reachabilityTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name: "trace_exposure_path",
			Description: "Trace bounded reachability from an internet-exposed handler through CALLS edges to a cloud sink " +
				"(privileged IAM action, readable secret, SQL table, or internet-exposed endpoint) from the curated " +
				"catalog. Findings are derived (symbol-level reachability, not value-flow) and use the conservative " +
				"truth-state vocabulary (exact/partial/ambiguous/unresolved). Never fabricates a path: when a " +
				"code-to-cloud bridge edge is not materialized, the cloud-sink segment is reported unresolved. " +
				"Provide source (handler name) with repo_id, or source_entity_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{
						"type":        "string",
						"description": "Source handler entity name (resolved within repo_id).",
					},
					"source_entity_id": map[string]any{
						"type":        "string",
						"description": "Source handler entity id (preferred when known).",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository id scoping source resolution by name.",
					},
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum CALLS traversal depth (1-10).",
						"default":     5,
						"minimum":     1,
						"maximum":     10,
					},
				},
			},
		},
	}
}
