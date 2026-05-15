package mcp

func iacManagementStatusTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_iac_management_status",
		Description: "Get the current read-only IaC management status and safety gate for one AWS stable resource identity. Provide scope_id or account_id plus arn or resource_id.",
		InputSchema: iacManagementStatusSchema(),
	}
}

func iacManagementExplanationTool() ToolDefinition {
	return ToolDefinition{
		Name:        "explain_iac_management_status",
		Description: "Explain one AWS IaC management status with grouped cloud, Terraform, raw-tag, management evidence, redaction, and safety gate details. Provide scope_id or account_id plus arn or resource_id.",
		InputSchema: iacManagementStatusSchema(),
	}
}

func terraformImportPlanTool() ToolDefinition {
	return ToolDefinition{
		Name:        "propose_terraform_import_plan",
		Description: "Generate read-only Terraform import-plan candidates from bounded AWS IaC management findings without running Terraform or mutating cloud state. Provide scope_id or account_id.",
		InputSchema: terraformImportPlanSchema(),
	}
}

func awsRuntimeDriftFindingsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_aws_runtime_drift_findings",
		Description: "List active AWS runtime drift reducer findings with bounded filters, truth outcomes, and rejected promotion status. Provide scope_id or account_id.",
		InputSchema: awsRuntimeDriftFindingsSchema(),
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
	}
}

func terraformImportPlanSchema() map[string]any {
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
				"description": "Optional exact AWS ARN to inspect",
			},
			"resource_id": map[string]any{
				"type":        "string",
				"description": "Optional alias for arn; for AWS this must be the full ARN, not a provider-local ID such as an S3 bucket name or Lambda function name",
			},
			"finding_kinds": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum candidate findings to inspect",
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
				"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource",
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
