// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func workItemTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_work_item_evidence",
			Description: "List bounded Jira/work-item source evidence by scope, project, work-item key, provider issue id, URL fingerprint, external URL, or observed-after window. Evidence remains source-only and does not verify pull request, commit, deployment, runtime artifact, image, version, service, or incident truth.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional Jira collector scope id.",
					},
					"project_key": map[string]any{
						"type":        "string",
						"description": "Optional Jira project key.",
					},
					"work_item_key": map[string]any{
						"type":        "string",
						"description": "Optional Jira issue/work-item key such as OPS-123.",
					},
					"provider_work_item_id": map[string]any{
						"type":        "string",
						"description": "Optional provider-native Jira issue id.",
					},
					"external_url": map[string]any{
						"type":        "string",
						"description": "Optional external URL to fingerprint server-side after sensitive query keys are removed. Raw URL is not returned.",
					},
					"url_fingerprint": map[string]any{
						"type":        "string",
						"description": "Optional sanitized URL fingerprint.",
					},
					"observed_after": map[string]any{
						"type":        "string",
						"description": "Optional RFC3339 observation lower bound.",
					},
					"after_fact_id": map[string]any{
						"type":        "string",
						"description": "Fact id from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum evidence rows to return.",
						"default":     25,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
	}
}
