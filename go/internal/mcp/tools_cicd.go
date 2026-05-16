package mcp

func cicdTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_ci_cd_run_correlations",
			Description: "List reducer-owned CI/CD run, artifact, and environment correlations by run, repository, commit, artifact digest, or environment.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional ingestion scope ID for a CI/CD run generation.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "CI/CD provider such as github_actions or gitlab_ci; required when provider_run_id is the only anchor.",
					},
					"provider_run_id": map[string]any{
						"type":        "string",
						"description": "Provider-native run, build, or pipeline ID for exact run lookup.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Repository ID to anchor run correlation lookup.",
					},
					"commit_sha": map[string]any{
						"type":        "string",
						"description": "Commit SHA to answer what CI/CD evidence exists after a commit.",
					},
					"artifact_digest": map[string]any{
						"type":        "string",
						"description": "Artifact or image digest to anchor artifact-to-run correlation lookup.",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Provider environment name to inspect environment observations without treating them as deployment truth.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer outcome filter.",
						"enum":        []string{"exact", "derived", "ambiguous", "unresolved", "rejected"},
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
