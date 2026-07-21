// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// infraResourceAggregateTools returns the cheap-summary aggregate tools
// shipped alongside the existing find_infra_resources search tool. They
// give callers an O(1) answer to ecosystem-level questions like "how many
// resources per provider?" without paging through the search endpoint.
//
// Hot-path performance requires the caller to narrow scope with at least a
// `category` filter (k8s / terraform / argocd / crossplane / helm / cloud) and one
// indexed-property predicate. Without scope the aggregate falls back to a
// multi-label scan across all documented infrastructure labels — correct,
// but slower; operators should treat that mode as a one-off check.
func infraResourceAggregateTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_infra_resources",
			Description: "Return graph-backed infrastructure resource totals for one optional scope without paging through individual resources. Provides total resources and rollups by provider, environment, and label (CloudResource / TerraformResource / TerraformStateResource / K8sResource / CloudFormationResource / ArgoCDApplication / CrossplaneXRD / HelmChart / etc.). Pass `category` (k8s / terraform / argocd / crossplane / helm / cloud) to narrow to one label-set for hot-path performance.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category": map[string]any{
						"type":        "string",
						"description": "Optional category to narrow the label set (k8s / terraform / argocd / crossplane / helm / cloud). Recommended for hot-path performance.",
						"enum":        []string{"k8s", "terraform", "argocd", "crossplane", "helm", "cloud"},
					},
					"kind":              map[string]any{"type": "string", "description": "Optional resource kind / resource_type filter."},
					"resource_type":     map[string]any{"type": "string", "description": "Optional Terraform/CloudFormation resource_type filter."},
					"provider":          map[string]any{"type": "string", "description": "Optional cloud provider filter (aws / gcp / azure / ...)."},
					"environment":       map[string]any{"type": "string", "description": "Optional environment filter (production / staging / development)."},
					"resource_service":  map[string]any{"type": "string", "description": "Optional resource_service filter (aws.ec2 / aws.s3 / k8s.workload / ...)."},
					"resource_category": map[string]any{"type": "string", "description": "Optional resource_category filter (compute / storage / network / ...)."},
				},
			},
		},
		{
			Name:        "get_infra_resource_inventory",
			Description: "Return a paginated grouped count of graph-backed infrastructure resources along one dimension (provider, environment, resource_category, resource_service, label). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions. Narrow with `category` for hot-path performance.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. provider (default) groups by cloud provider; environment groups by deployment environment; resource_category groups by category; resource_service groups by service; label groups by node label (TerraformResource / TerraformStateResource / K8sResource / ...).",
						"enum":        []string{"provider", "environment", "resource_category", "resource_service", "label"},
						"default":     "provider",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Optional category to narrow the label set. Recommended for hot-path performance.",
						"enum":        []string{"k8s", "terraform", "argocd", "crossplane", "helm", "cloud"},
					},
					"kind":              map[string]any{"type": "string"},
					"resource_type":     map[string]any{"type": "string"},
					"provider":          map[string]any{"type": "string"},
					"environment":       map[string]any{"type": "string"},
					"resource_service":  map[string]any{"type": "string"},
					"resource_category": map[string]any{"type": "string"},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum buckets to return per page.",
						"default":     100,
						"minimum":     1,
						"maximum":     500,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging.",
						"default":     0,
						"minimum":     0,
						"maximum":     10000,
					},
				},
			},
		},
	}
}
