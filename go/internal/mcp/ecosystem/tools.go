// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecosystemtools

import "github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"

// Tools returns the ecosystem tool definitions in their canonical order.
func Tools() []toolcontract.ToolDefinition {
	tools := ecosystemOverviewTools()
	tools = append(tools, graphSummaryPacketTool(), contractImpactTool())
	tools = append(tools, deploymentTools()...)
	tools = append(tools, infraResourceSearchTool())
	tools = append(tools, infrastructureTools()...)
	tools = append(tools, repositoryTools()...)
	tools = append(tools, preChangeImpactTool(), developerChangePlanTool())
	tools = append(tools, packageRegistryDefinitionTools()...)
	tools = append(tools, repositoryImpactTools()...)
	tools = append(tools, findChangeSurfaceTool(), investigateChangeSurfaceTool())
	tools = append(tools, compareEnvironmentTools()...)
	return tools
}

func ecosystemOverviewTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "get_ecosystem_overview",
			Description: "Get a high-level overview of the indexed ecosystem: repos, tiers, infrastructure counts, and cross-repo relationships.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
	}
}

func deploymentTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "trace_deployment_chain",
			Description: "Trace the full deployment chain for a service across ArgoCD Applications and ApplicationSets.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_name": map[string]any{
						"type":        "string",
						"description": "Name of the service to trace",
					},
					"direct_only": map[string]any{
						"type":        "boolean",
						"description": "Whether to return only direct relationships",
						"default":     true,
					},
					// #5720 round-7 P1-5/P2-4: this schema advertises no
					// minimum, maximum, or default on purpose. The handler
					// clamps rather than rejects (see
					// normalizeTraceDeploymentChainMaxDepth in
					// go/internal/query/impact_trace_deployment.go), and the
					// route's OpenAPI fragment says so explicitly. A
					// JSON-Schema minimum/maximum makes a validating MCP
					// client reject exactly the out-of-range values the
					// server happily clamps -- reintroducing the 400 that
					// clamping was chosen to avoid. A "default": 8 was worse:
					// a client applying advertised defaults would start
					// sending max_depth: 8, resolving to a search limit of 80
					// instead of the handler's own operator-safe 25, for a
					// caller that changed nothing. The bounds live in the
					// description, where they inform without gating.
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum depth to traverse. Scales the indirect-evidence search limit (max_depth x 10, capped at 100); it is not a literal traversal-hop count. Out-of-range values are clamped to 0-1000 rather than rejected. Omitting this field does not apply a default -- it resolves to the handler's own operator-safe default search limit of 25.",
					},
					"include_related_module_usage": map[string]any{
						"type":        "boolean",
						"description": "Whether to include related Terraform module usage",
						"default":     false,
					},
				},
				"required": []string{"service_name"},
			},
		},
		{
			Name:        "investigate_deployment_config",
			Description: "Return a bounded story of the files, repositories, values layers, image tag sources, runtime settings, resource limits, and rendered targets that influence a service deployment. Provide service_name or workload_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_name": map[string]any{
						"type":        "string",
						"description": "Service name to investigate.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Canonical workload id to investigate when service_name is not enough.",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment scope such as platform-qa or prod.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum rows per response section.",
						"default":     25,
						"minimum":     1,
						"maximum":     100,
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "find_blast_radius",
			Description: "Find all repos and resources affected by changing a target.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Target entity to analyze",
					},
					"target_type": map[string]any{
						"type":        "string",
						"description": "Type of the target entity",
						"enum":        []string{"repository", "terraform_module", "crossplane_xrd", "sql_table"},
						"default":     "repository",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum affected rows to return",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"target"},
			},
		},
	}
}

func infrastructureTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "investigate_resource",
			Description: "Resolve a queue, database, cloud resource, Terraform resource, or Kubernetes object into a bounded investigation packet with workload users, provisioning repositories, source handles, ambiguity metadata, and next calls. Provide query or resource_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Resource name, kind, queue, database, or cloud identifier to resolve.",
					},
					"resource_id": map[string]any{
						"type":        "string",
						"description": "Canonical graph resource id when already known.",
					},
					"resource_type": map[string]any{
						"type":        "string",
						"description": "Optional resource family to narrow resolution.",
						"enum":        []string{"queue", "database", "cloud_resource", "k8s_resource", "terraform_resource", "terraform_module"},
						"default":     "",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment scope.",
					},
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum repository provenance traversal depth.",
						"default":     4,
						"minimum":     1,
						"maximum":     8,
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum rows per response section.",
						"default":     25,
						"minimum":     1,
						"maximum":     100,
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "analyze_infra_relationships",
			Description: "Analyze infrastructure relationships: what deploys what, what provisions what, what image a workload runs, what workloads run an image, or what image a Lambda function uses.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query_type": map[string]any{
						"type":        "string",
						"description": "Type of infrastructure relationship to analyze",
						"enum":        []string{"what_deploys", "what_provisions", "who_consumes_xrd", "module_consumers", "what_runs_image", "what_runs_lambda_image"},
					},
					"target": map[string]any{
						"type":        "string",
						"description": "Target infrastructure entity",
					},
				},
				"required": []string{"query_type", "target"},
			},
		},
	}
}

func repositoryTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "get_repo_summary",
			Description: "Get a lightweight identity and coverage summary for a repository: file count, languages, entity count, entity types, and indexing coverage state. Use this for a quick overview before calling get_repo_context, which returns the full enriched context including entry points, infrastructure, relationships, API surface, and deployment evidence. Provide exactly one of repo_id (preferred) or repo_name; the call is rejected if neither is set.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository selector: canonical ID, name, repo slug, or indexed path. Provide either repo_id (preferred) or the legacy repo_name; repo_id takes priority when both are set.",
					},
					"repo_name": map[string]any{
						"type":        "string",
						"description": "Deprecated alias for repo_id, accepted for backward compatibility. Provide either repo_id (preferred) or repo_name; repo_id takes priority when both are set.",
					},
				},
			},
		},
		{
			Name:        "get_repo_context",
			Description: "Get complete context for a repository in a single call. Accepts a repository selector such as canonical ID, name, repo slug, or indexed path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository selector: canonical ID, name, repo slug, or indexed path",
					},
				},
				"required": []string{"repo_id"},
			},
		},
		{
			Name:        "get_relationship_evidence",
			Description: "Dereference a relationship evidence pointer by resolved_id and return durable source/target metadata, confidence_basis, evidence kinds, rationale, and preview details.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resolved_id": map[string]any{
						"type":        "string",
						"description": "resolved_relationships.resolved_id returned by deployment evidence artifacts or evidence_index",
					},
				},
				"required": []string{"resolved_id"},
			},
		},
	}
}

func repositoryImpactTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "get_repo_story",
			Description: "Get a structured story for a repository. Accepts a repository selector such as canonical ID, name, repo slug, or indexed path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository selector: canonical ID, name, repo slug, or indexed path",
					},
				},
				"required": []string{"repo_id"},
			},
		},
		{
			Name:        "get_repository_coverage",
			Description: "Get repository-scoped durable coverage and completeness data for one repository selector.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository selector: canonical ID, name, repo slug, or indexed path",
					},
				},
				"required": []string{"repo_id"},
			},
		},
		{
			Name:        "trace_resource_to_code",
			Description: "Trace an infrastructure resource back to the code and repositories that own or configure it.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"start": map[string]any{
						"type":        "string",
						"description": "Starting resource identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment to scope the trace",
					},
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum traversal depth",
						"default":     8,
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum repository paths to return",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"start"},
			},
		},
		{
			Name:        "explain_dependency_path",
			Description: "Explain the dependency path between two canonical entities.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{
						"type":        "string",
						"description": "Source entity identifier",
					},
					"target": map[string]any{
						"type":        "string",
						"description": "Target entity identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
				},
				"required": []string{"source", "target"},
			},
		},
	}
}

func compareEnvironmentTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "compare_environments",
			Description: "Compare the dependency surface for a workload across two environments with shared, dedicated, evidence, limitation, and next-call story fields.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Canonical workload identifier",
					},
					"left": map[string]any{
						"type":        "string",
						"description": "First environment to compare",
					},
					"right": map[string]any{
						"type":        "string",
						"description": "Second environment to compare",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum cloud resources to read per environment",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"workload_id", "left", "right"},
			},
		},
	}
}
