// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iacmanagementtools

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Tools returns the five MCP tool definitions this package owns, in their
// long-standing registration order: the read-only management-status pair,
// the Terraform import-plan proposer, the Terraform config-vs-state drift
// finder, and the replatforming-ownership packet builder. The root package
// splices them into ReadOnlyTools at their long-standing positions via
// indexed access, so the client-visible registration order is unchanged.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		managementStatusTool(),
		managementExplanationTool(),
		importPlanTool(),
		configStateDriftFindingsTool(),
		ownershipTool(),
	}
}

// FindingKindsDescription documents the optional finding-kinds selector
// shared by the IaC-management schemas in this package. It lives here, not
// at the root, so the wording has one owner; the root keeps using it for the
// find_unmanaged_resources schema it still defines inline.
const FindingKindsDescription = "Optional finding kinds. When omitted, defaults to actionable existence findings: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, and ambiguous_cloud_resource. Explicitly select image_version_drift or value_comparison_inconclusive to include managed value drift or degraded comparison evidence."

func managementStatusTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "get_iac_management_status",
		Description: "Get the current read-only IaC management status and safety gate for one AWS stable resource identity. Provide scope_id or account_id plus arn or resource_id.",
		InputSchema: managementStatusSchema(),
	}
}

func managementExplanationTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "explain_iac_management_status",
		Description: "Explain one AWS IaC management status with grouped cloud, Terraform, raw-tag, management evidence, redaction, and safety gate details. Provide scope_id or account_id plus arn or resource_id.",
		InputSchema: managementStatusSchema(),
	}
}

func importPlanTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "propose_terraform_import_plan",
		Description: "Generate read-only Terraform import-plan candidates from bounded AWS IaC management findings without running Terraform or mutating cloud state. Provide scope_id or account_id.",
		InputSchema: importPlanSchema(),
	}
}

func configStateDriftFindingsTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "list_terraform_config_state_drift_findings",
		Description: "List active Terraform config-vs-state drift reducer findings for one bounded state-snapshot scope, with the exact/derived/ambiguous/unresolved outcome and drift kind for each finding. \"derived\" means the finding's address may not match the real Terraform state address because it, or its paired config-side/state-side counterpart for the same resource, depended on an unresolved module-prefix fallback (a Terraform-Registry-shorthand misclassification, e.g. a repo whose top-level directory is literally \"terraform-aws-modules\", or a module chain deeper than the resolver's depth bound); both halves of a spurious added_in_config/added_in_state pair carry \"derived\" when the pairing is unambiguous, not only the config-side half -- check the finding's evidence array for the specific reason before trusting it as genuine drift. \"unresolved\" means backend-owner resolution found zero candidate config repos AT EVALUATION TIME, not that no owner exists: an unsynced repo and a genuinely untracked backend look identical today, and a later evaluation of the same generation can resolve to exact or ambiguous instead once an owning repo is present. A scope that has not resolved at evaluation time is reported as one \"unresolved\" finding, not an empty page, so it can be told apart from a scope that resolved cleanly and simply has no drift. Provider-neutral: config-vs-state drift is not cloud-specific. Provide scope_id.",
		InputSchema: configStateDriftFindingsSchema(),
	}
}

func ownershipTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "find_unmanaged_resource_owners",
		Description: "For each active AWS drift finding, compose a bounded ownership packet of owner, repository, module, service, and environment candidates with explicit ambiguity reasons, confidence, freshness, and the read-only safety gate. Candidates come from reducer-owned fields only; a single candidate is derived, never exact, and conflicting candidates are surfaced with ambiguity reasons rather than collapsed to a single guessed owner. Raw tags stay provenance-only and never become owner candidates. Provide scope_id or account_id.",
		InputSchema: ownershipSchema(),
	}
}

func ownershipSchema() map[string]any {
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
				"description": FindingKindsDescription,
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

func managementStatusSchema() map[string]any {
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
				"description": FindingKindsDescription,
			},
		},
	}
}

func importPlanSchema() map[string]any {
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
				"description": FindingKindsDescription,
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

func configStateDriftFindingsSchema() map[string]any {
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
				"description": "Optional outcome filter: exact, derived, ambiguous, or unresolved",
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
