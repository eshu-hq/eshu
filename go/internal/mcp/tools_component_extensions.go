// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func componentExtensionTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_component_extensions",
			Description: "Return component extension inventory with installed, enabled, claim-capable, failed, revoked, and incompatible states from API registry readback.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of component rows to return",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "get_component_extension_diagnostics",
			Description: "Return sanitized diagnostics for one installed component extension, including component ID, version, digest, lifecycle states, and policy status.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"component_id": map[string]any{
						"type":        "string",
						"description": "Component package ID",
					},
				},
				"required": []string{"component_id"},
			},
		},
	}
}
