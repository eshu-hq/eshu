// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func admissionDecisionTools() []ToolDefinition {
	return []ToolDefinition{{
		Name:        "list_admission_decisions",
		Description: "List reducer-owned correlation admission decisions by domain, scope, generation, state, or anchor. These rows explain admitted, rejected, ambiguous, stale, missing-evidence, and permission-hidden candidates before or beside canonical graph edges; they are not themselves canonical graph edges.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"domain": map[string]any{
					"type":        "string",
					"description": "Reducer admission domain, such as deployable_unit, cloud_inventory, or package_source.",
				},
				"scope_id": map[string]any{
					"type":        "string",
					"description": "Ingestion scope id that bounds the admission decision read.",
				},
				"generation_id": map[string]any{
					"type":        "string",
					"description": "Scope generation id that bounds the admission decision read.",
				},
				"state": map[string]any{
					"type": "string",
					"enum": []string{
						"admitted",
						"rejected",
						"ambiguous",
						"stale",
						"missing_evidence",
						"permission_hidden",
						"unsupported",
						"unsafe",
					},
					"description": "Optional shared admission state filter.",
				},
				"anchor_kind": map[string]any{
					"type":        "string",
					"description": "Optional anchor kind, for example service, repository, workload, cloud_resource, package, or incident.",
				},
				"anchor_id": map[string]any{
					"type":        "string",
					"description": "Optional anchor id. Provide with anchor_kind.",
				},
				"include_evidence": map[string]any{
					"type":        "boolean",
					"description": "When true, include bounded evidence rows for returned decisions.",
					"default":     false,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum decisions to return.",
					"default":     50,
					"minimum":     1,
					"maximum":     200,
				},
			},
			"required": []string{"domain", "scope_id", "generation_id"},
		},
	}}
}
