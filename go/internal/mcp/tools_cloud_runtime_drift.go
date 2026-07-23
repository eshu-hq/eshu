// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// cloudRuntimeDriftTools returns the provider-neutral multi-cloud runtime drift
// readback tool (issues #1997, #1998). It mirrors the
// POST /api/v0/cloud/runtime-drift/findings route: a bounded, paginated,
// truth-labeled list of reducer-owned reducer_multi_cloud_runtime_drift_finding
// rows filterable by provider, canonical scope, canonical resource uid, and
// finding_kind. The tool is read-only and never returns raw provider locators or
// raw evidence atoms; unsafe findings are reported as rejected, not omitted.
func cloudRuntimeDriftTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_cloud_runtime_drift_findings",
			Description: "List provider-neutral multi-cloud runtime drift findings (reducer_multi_cloud_runtime_drift_finding) for a bounded canonical scope across aws, gcp, and azure. Filterable by provider, canonical cloud_resource_uid, and finding_kind. Returns provider, normalized identity, finding_kind, management_status, provider-neutral source state, and refusal-safety posture; unsafe findings are reported as rejected, not omitted. Unsupported on lightweight local runtime.",
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
						"description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, ambiguous_cloud_resource, or image_version_drift",
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
