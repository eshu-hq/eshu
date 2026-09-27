// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	iacmanagementtools "github.com/eshu-hq/eshu/go/internal/mcp/iac/management"
)

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
				"description": iacmanagementtools.FindingKindsDescription,
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
				"description": iacmanagementtools.FindingKindsDescription,
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
