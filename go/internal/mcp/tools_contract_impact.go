// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func contractImpactTool() ToolDefinition {
	return ToolDefinition{
		Name:        "investigate_contract_impact",
		Description: "Investigate deterministic cross-repository API contract impact without string similarity inference. HTTP provider evidence is supported; topic and grpc return explicit unsupported family states until deterministic projections land.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"family": map[string]any{
					"type":        "string",
					"description": "Contract family to inspect.",
					"enum":        []string{"http", "topic", "grpc"},
					"default":     "http",
				},
				"provider_repo_id": map[string]any{
					"type":        "string",
					"description": "Repository that exposes the contract.",
				},
				"consumer_repo_id": map[string]any{
					"type":        "string",
					"description": "Repository expected to consume the contract.",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Alias for provider_repo_id on provider-side HTTP lookups.",
				},
				"route": map[string]any{
					"type":        "string",
					"description": "HTTP route path for provider-side HTTP contract lookup.",
				},
				"topic": map[string]any{
					"type":        "string",
					"description": "Topic or queue name for deferred topic contract family lookup.",
				},
				"service_name": map[string]any{
					"type":        "string",
					"description": "gRPC/protobuf service name for deferred grpc contract family lookup.",
				},
				"method": map[string]any{
					"type":        "string",
					"description": "Optional HTTP method filter.",
					"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum contract rows to return.",
					"default":     25,
					"minimum":     1,
					"maximum":     100,
				},
			},
			"required": []string{},
		},
	}
}
