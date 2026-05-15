package mcp

func iacManagementStatusTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_iac_management_status",
		Description: "Get the current read-only IaC management status and safety gate for one AWS stable resource identity.",
		InputSchema: iacManagementStatusSchema(),
	}
}

func iacManagementExplanationTool() ToolDefinition {
	return ToolDefinition{
		Name:        "explain_iac_management_status",
		Description: "Explain one AWS IaC management status with grouped cloud, Terraform, raw-tag, management evidence, redaction, and safety gate details.",
		InputSchema: iacManagementStatusSchema(),
	}
}

func iacManagementStatusSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope_id": map[string]any{
				"type":        "string",
				"description": "Exact AWS collector scope, for example aws:123456789012:us-east-1:lambda",
			},
			"account_id": map[string]any{
				"type":        "string",
				"description": "AWS account ID used to bound the active finding read",
			},
			"region": map[string]any{
				"type":        "string",
				"description": "Optional AWS region when account_id is supplied",
			},
			"arn": map[string]any{
				"type":        "string",
				"description": "Exact AWS ARN to inspect",
			},
			"resource_id": map[string]any{
				"type":        "string",
				"description": "Provider-stable resource identity; for AWS pass the ARN",
			},
			"finding_kinds": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource",
			},
		},
		"anyOf": []map[string]any{
			{"required": []string{"scope_id", "arn"}},
			{"required": []string{"scope_id", "resource_id"}},
			{"required": []string{"account_id", "arn"}},
			{"required": []string{"account_id", "resource_id"}},
		},
	}
}
