// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func kubernetesTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_kubernetes_correlations",
			Description: "List reducer-owned Kubernetes workload ownership and drift correlations by cluster, workload object, namespace, image reference, source digest, or scope.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional reducer scope ID for a Kubernetes correlation generation.",
					},
					"cluster_id": map[string]any{
						"type":        "string",
						"description": "Cluster ID to anchor Kubernetes correlation lookup.",
					},
					"workload_object_id": map[string]any{
						"type":        "string",
						"description": "Durable workload object ID emitted by the Kubernetes live collector (an opaque deterministic identifier, not a deployment/namespace/name shorthand) to anchor lookup.",
					},
					"namespace": map[string]any{
						"type":        "string",
						"description": "Kubernetes namespace to anchor lookup.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Live container image reference to anchor lookup.",
					},
					"source_digest": map[string]any{
						"type":        "string",
						"description": "Deployment-source image digest to anchor lookup.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer outcome filter.",
						"enum":        []string{"exact", "derived", "ambiguous", "unresolved", "stale", "rejected"},
					},
					"drift_kind": map[string]any{
						"type":        "string",
						"description": "Optional drift kind filter such as in_sync, image_drift, missing_source, or stale_source.",
					},
					"after_correlation_id": map[string]any{
						"type":        "string",
						"description": "Correlation ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum correlation rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
	}
}
