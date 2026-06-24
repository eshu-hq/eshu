// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// cloudInventoryTools returns the canonical multi-cloud resource inventory
// readback tool. It mirrors the GET /api/v0/cloud/inventory route: a bounded,
// paginated, truth-labeled list of reducer-owned reducer_cloud_resource_identity
// rows filterable by provider, canonical scope, and management_origin. The tool
// is read-only and never returns raw provider locators, raw actors, raw
// identities, tags, assignment scopes, or credentials.
func cloudInventoryTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_cloud_resource_inventory",
			Description: "List canonical multi-cloud resource identities (reducer_cloud_resource_identity) by bounded provider, scope, and management_origin filters. Returns provider, normalized identity, management_origin, per-layer evidence flags, provider-neutral source state, optional keyed tag fingerprints, optional bounded identity-policy evidence, and optional sanitized freshness evidence. Unsupported on lightweight local runtime.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"provider": map[string]any{
						"type":        "string",
						"description": "Cloud provider filter: aws, gcp, or azure",
						"enum":        []string{"aws", "gcp", "azure"},
					},
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Canonical scope id filter",
					},
					"account_id": map[string]any{
						"type":        "string",
						"description": "Alias for scope_id (AWS account scope)",
					},
					"project_id": map[string]any{
						"type":        "string",
						"description": "Alias for scope_id (GCP project scope)",
					},
					"subscription_id": map[string]any{
						"type":        "string",
						"description": "Alias for scope_id (Azure subscription scope)",
					},
					"management_origin": map[string]any{
						"type":        "string",
						"description": "Strongest contributing evidence layer: declared, applied, or observed",
						"enum":        []string{"declared", "applied", "observed"},
					},
					"limit": map[string]any{
						"type":    "integer",
						"default": 50,
						"minimum": 1,
						"maximum": 200,
					},
					"cursor": map[string]any{
						"type":        "string",
						"description": "Continuation cursor: non-negative integer offset from the previous page's next_cursor",
					},
				},
				"required": []string{},
			},
		},
	}
}
