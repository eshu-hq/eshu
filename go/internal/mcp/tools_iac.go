// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func awsRuntimeDriftFindingsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_aws_runtime_drift_findings",
		Description: "List active AWS runtime drift reducer findings with bounded filters, truth outcomes, and rejected promotion status. Provide scope_id or account_id.",
		InputSchema: awsRuntimeDriftFindingsSchema(),
	}
}

func awsRuntimeDriftFindingsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope_id": map[string]any{
				"type":        "string",
				"description": "Exact AWS collector scope, for example aws:123456789012:us-east-1:lambda",
			},
			"account_id": map[string]any{
				"type":        "string",
				"description": "AWS account ID used to bound the active drift finding read",
			},
			"region": map[string]any{
				"type":        "string",
				"description": "Optional AWS region when account_id is supplied",
			},
			"arn": map[string]any{
				"type":        "string",
				"description": "Optional exact AWS ARN to inspect",
			},
			"finding_kinds": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, ambiguous_cloud_resource, image_version_drift, or value_comparison_inconclusive",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum drift findings to return",
				"default":     100,
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Zero-based result offset for paging findings",
				"default":     0,
			},
		},
	}
}
