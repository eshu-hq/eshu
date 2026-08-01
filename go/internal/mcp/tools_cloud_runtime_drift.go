// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// cloudRuntimeDriftTools returns the runtime drift readback tool spanning all
// three providers (issues #1997, #1998, #5759 follow-up). It mirrors the
// POST /api/v0/cloud/runtime-drift/findings route: a bounded, paginated,
// truth-labeled list aggregating reducer-owned
// reducer_multi_cloud_runtime_drift_finding rows (gcp, azure) with
// reducer_aws_cloud_runtime_drift_finding rows (aws) in one query, filterable
// by provider, canonical scope, canonical resource uid, and finding_kind.
// provider=aws and an unfiltered query both return real AWS findings, not the
// empty page this tool returned for aws before the aggregation existed. The
// tool is read-only and never returns raw provider locators (including the
// AWS ARN) or raw evidence atoms; unsafe findings are reported as rejected,
// not omitted.
func cloudRuntimeDriftTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_cloud_runtime_drift_findings",
			Description: "List runtime drift findings for a bounded canonical scope across aws, gcp, and azure, aggregating reducer_multi_cloud_runtime_drift_finding (gcp, azure) and reducer_aws_cloud_runtime_drift_finding (aws) in one query. Filterable by provider, canonical cloud_resource_uid, and finding_kind; cloud_resource_uid filtering matches only gcp/azure findings. Returns provider, normalized identity, finding_kind, management_status, provider-neutral source state, and refusal-safety posture; an aws-origin finding's safety verdict is derived through the same classification list_aws_runtime_drift_findings uses, so the identical row never disagrees across the two tools. Unsafe findings are reported as rejected, not omitted. Unsupported on lightweight local runtime.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Canonical ingestion scope id (required unless an account/project/subscription alias is set)",
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
					"provider": map[string]any{
						"type":        "string",
						"description": "Cloud provider filter: aws, gcp, or azure",
						"enum":        []string{"aws", "gcp", "azure"},
					},
					"cloud_resource_uid": map[string]any{
						"type":        "string",
						"description": "Optional exact canonical resource uid to inspect",
					},
					"finding_kinds": map[string]any{
						"type":        "array",
						"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, ambiguous_cloud_resource, image_version_drift, or value_comparison_inconclusive",
						"items":       map[string]any{"type": "string"},
					},
					"limit": map[string]any{
						"type":    "integer",
						"default": 100,
						"minimum": 1,
						"maximum": 500,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging findings",
						"minimum":     0,
					},
				},
				"required": []string{},
			},
		},
	}
}
