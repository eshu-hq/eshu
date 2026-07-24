// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func composeReplatformingPlanTool() ToolDefinition {
	return ToolDefinition{
		Name:        "compose_replatforming_plan",
		Description: "Compose one bounded, truth-labeled replatforming plan for a service or account scope from active AWS IaC management findings, with per-item source state, safety gate, owner candidates, and ready or refused Terraform import candidates. Items are ordered into deterministic migration waves (early-safe, review, then blocked last) and blast-radius groups from dependency and missing-evidence signals the findings already carry. Read-only: never runs Terraform, imports resources, or mutates cloud state. Provide scope_kind plus scope_id or account_id.",
		InputSchema: composeReplatformingPlanSchema(),
	}
}

func awsRuntimeDriftFindingsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_aws_runtime_drift_findings",
		Description: "List active AWS runtime drift reducer findings with bounded filters, truth outcomes, and rejected promotion status. Provide scope_id or account_id.",
		InputSchema: awsRuntimeDriftFindingsSchema(),
	}
}

func replatformingRollupsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_replatforming_rollups",
		Description: "Summarize replatforming drift and readiness as bounded rollups by account, environment, and service over the provider-neutral source-state taxonomy (exact, derived, partial, ambiguous, stale, unavailable, unsupported, unknown, rejected) plus an import-ready vs needs-review vs refused readiness view. Ambiguous or missing attribution is counted under explicit buckets, never guessed. Provide scope_id or account_id.",
		InputSchema: replatformingRollupsSchema(),
	}
}

func replatformingOwnershipTool() ToolDefinition {
	return ToolDefinition{
		Name:        "find_unmanaged_resource_owners",
		Description: "For each active AWS drift finding, compose a bounded ownership packet of owner, repository, module, service, and environment candidates with explicit ambiguity reasons, confidence, freshness, and the read-only safety gate. Candidates come from reducer-owned fields only; a single candidate is derived, never exact, and conflicting candidates are surfaced with ambiguity reasons rather than collapsed to a single guessed owner. Raw tags stay provenance-only and never become owner candidates. Provide scope_id or account_id.",
		InputSchema: replatformingOwnershipSchema(),
	}
}

func replatformingOwnershipSchema() map[string]any {
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
			"finding_kinds": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum findings to compose into the bounded page",
				"default":     100,
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Zero-based result offset for paging the bounded page",
				"default":     0,
			},
		},
	}
}

func replatformingRollupsSchema() map[string]any {
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
			"finding_kinds": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum findings to aggregate into the bounded rollup",
				"default":     100,
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Zero-based result offset for paging the bounded rollup",
				"default":     0,
			},
		},
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

func composeReplatformingPlanSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope_kind": map[string]any{
				"type":        "string",
				"description": "Primary plan scope dimension: account, region, service, workload, repository, environment, or resource",
				"enum":        []string{"account", "region", "service", "workload", "repository", "environment", "resource"},
			},
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
			"service_name": map[string]any{
				"type":        "string",
				"description": "Optional service name that narrows the plan scope",
			},
			"workload_id": map[string]any{
				"type":        "string",
				"description": "Optional deployable workload identity that narrows the plan scope",
			},
			"repo_id": map[string]any{
				"type":        "string",
				"description": "Optional source repository identity that narrows the plan scope",
			},
			"environment": map[string]any{
				"type":        "string",
				"description": "Optional environment that narrows the plan scope",
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
				"description": "Maximum migration packet items to compose",
				"default":     100,
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Zero-based result offset for paging migration packet items",
				"default":     0,
			},
		},
	}
}

func terraformConfigStateDriftFindingsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_terraform_config_state_drift_findings",
		Description: "List active Terraform config-vs-state drift reducer findings for one bounded state-snapshot scope, with the exact/ambiguous outcome and drift kind for each finding. Provider-neutral: config-vs-state drift is not cloud-specific. Provide scope_id.",
		InputSchema: terraformConfigStateDriftFindingsSchema(),
	}
}

func terraformConfigStateDriftFindingsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope_id": map[string]any{
				"type":        "string",
				"description": "Exact Terraform state-snapshot scope, for example state_snapshot:s3:hash-1",
			},
			"address": map[string]any{
				"type":        "string",
				"description": "Optional exact Terraform resource address to inspect",
			},
			"outcome": map[string]any{
				"type":        "string",
				"description": "Optional outcome filter: exact or ambiguous",
			},
			"drift_kinds": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional drift kinds: added_in_state, added_in_config, attribute_drift, removed_from_state, or removed_from_config",
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
		"required": []string{"scope_id"},
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
				"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, ambiguous_cloud_resource, or image_version_drift",
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
