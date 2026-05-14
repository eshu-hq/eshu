package mcp

func ecosystemTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_ecosystem_overview",
			Description: "Get a high-level overview of the indexed ecosystem: repos, tiers, infrastructure counts, and cross-repo relationships.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
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
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum depth to traverse",
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
		{
			Name:        "find_infra_resources",
			Description: "Search infrastructure resources (K8s, Terraform, ArgoCD, Crossplane, Helm) by name or type.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query for infrastructure resources",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Category of infrastructure to search",
						"enum":        []string{"k8s", "terraform", "argocd", "crossplane", "helm"},
						"default":     "",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum infrastructure resources to return",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "analyze_infra_relationships",
			Description: "Analyze infrastructure relationships: what deploys what, what provisions what.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query_type": map[string]any{
						"type":        "string",
						"description": "Type of infrastructure relationship to analyze",
						"enum":        []string{"what_deploys", "what_provisions", "who_consumes_xrd", "module_consumers"},
					},
					"target": map[string]any{
						"type":        "string",
						"description": "Target infrastructure entity",
					},
				},
				"required": []string{"query_type", "target"},
			},
		},
		{
			Name:        "get_repo_summary",
			Description: "Get a structured summary of a repository.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_name": map[string]any{
						"type":        "string",
						"description": "Name of the repository",
					},
				},
				"required": []string{"repo_name"},
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
			Description: "Dereference a relationship evidence pointer by resolved_id and return the durable source/target metadata, evidence kinds, rationale, and preview details.",
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
		{
			Name:        "list_package_registry_packages",
			Description: "List package registry package identities by package_id or ecosystem/name without inferring repository ownership.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package_id": map[string]any{
						"type":        "string",
						"description": "Exact Package.uid lookup.",
					},
					"ecosystem": map[string]any{
						"type":        "string",
						"description": "Package ecosystem scope such as npm, maven, pypi, go, cargo, or nuget.",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Normalized package name. Requires ecosystem when package_id is absent.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum packages to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"limit"},
			},
		},
		{
			Name:        "list_package_registry_versions",
			Description: "List package registry version identities for one Package.uid without inferring repository ownership.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package_id": map[string]any{
						"type":        "string",
						"description": "Package.uid to anchor the version lookup.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum versions to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"package_id", "limit"},
			},
		},
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
		{
			Name:        "find_change_surface",
			Description: "Find the blast or change surface for a workload, cloud resource, or terraform module.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Target entity identifier",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum impacted rows to return",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "investigate_change_surface",
			Description: "Investigate the code, repository, workload, infrastructure, and transitive impact surface for a service, module, resource, code topic, or changed path set in one bounded call.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Optional canonical entity id or exact entity name to resolve before impact traversal.",
					},
					"target_type": map[string]any{
						"type":        "string",
						"description": "Optional target kind used to choose the exact resolver shape.",
						"enum":        []string{"service", "workload", "workload_instance", "repository", "resource", "cloud_resource", "terraform_module", "module"},
					},
					"service_name": map[string]any{
						"type":        "string",
						"description": "Service or workload name to resolve as the graph impact anchor.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Canonical workload id to resolve as the graph impact anchor.",
					},
					"resource_id": map[string]any{
						"type":        "string",
						"description": "Canonical cloud resource id to resolve as the graph impact anchor.",
					},
					"module_id": map[string]any{
						"type":        "string",
						"description": "Terraform module uid or name to resolve as the graph impact anchor.",
					},
					"topic": map[string]any{
						"type":        "string",
						"description": "Natural-language code topic such as repo-sync auth behavior.",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository selector for code-topic and changed-path scoping.",
					},
					"changed_paths": map[string]any{
						"type":        "array",
						"description": "Changed file paths to map to touched code symbols.",
						"items":       map[string]any{"type": "string"},
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment filter for graph impact rows.",
					},
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum graph traversal depth.",
						"default":     4,
						"minimum":     1,
						"maximum":     8,
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum rows to return per surface.",
						"default":     25,
						"minimum":     1,
						"maximum":     100,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Result offset for content-backed code investigation.",
						"default":     0,
						"minimum":     0,
						"maximum":     10000,
					},
				},
				"required": []string{},
			},
		},
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
