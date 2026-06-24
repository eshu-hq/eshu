// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func cicdTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_ci_cd_run_correlations",
			Description: "List reducer-owned CI/CD run, artifact, and environment correlations by run, repository, commit, artifact digest, image reference, or environment. Repository-scoped results include an evidence_summary that separates static workflow files, live run rows, run-to-artifact/image bridge evidence, and public-safe missing-hop classes. Live run rows come from the opt-in ci_cd_run collector polling the provider run API such as GitHub Actions (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus a provider token), so a default git-only deploy returns only static workflow evidence and no run correlation rows.",
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
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Image reference to anchor tag-or-reference based run correlation lookup.",
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
