// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func infraResourceSearchTool() ToolDefinition {
	return ToolDefinition{
		Name:        "find_infra_resources",
		Description: "Search infrastructure resources (cloud, K8s, Terraform, ArgoCD, Crossplane, Helm) by text or structured scope.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Optional search query for infrastructure resources. Omit when using structured filters.",
				},
				"category": map[string]any{
					"type":        "string",
					"description": "Category of infrastructure to search",
					"enum":        []string{"k8s", "terraform", "argocd", "crossplane", "helm", "cloud"},
					"default":     "",
				},
				"kind": map[string]any{
					"type":        "string",
					"description": "Optional infrastructure kind, resource_type, data_type, or service_kind filter.",
				},
				"provider": map[string]any{
					"type":        "string",
					"description": "Optional provider filter such as aws, kubernetes, or terraform.",
				},
				"environment": map[string]any{
					"type":        "string",
					"description": "Optional deployment environment filter.",
				},
				"resource_service": map[string]any{
					"type":        "string",
					"description": "Optional resource_service or cloud service_kind filter such as ec2 or s3.",
				},
				"resource_category": map[string]any{
					"type":        "string",
					"description": "Optional resource_category filter such as compute, storage, or networking.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum infrastructure resources to return",
					"default":     50,
					"minimum":     1,
					"maximum":     200,
				},
			},
			"required": []string{},
		},
	}
}
