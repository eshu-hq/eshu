// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func packageRegistryTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_package_registry_dependencies",
			Description: "List package-native dependency edges by Package.uid or PackageVersion.uid without inferring repository ownership or runtime consumption. Populated by the opt-in package_registry collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus registry credentials), so a default git-only deploy returns an empty page.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package_id": map[string]any{
						"type":        "string",
						"description": "Package.uid to anchor dependency lookup when version_id is absent.",
					},
					"version_id": map[string]any{
						"type":        "string",
						"description": "PackageVersion.uid for an exact version-scoped dependency lookup.",
					},
					"after_version_id": map[string]any{
						"type":        "string",
						"description": "Source PackageVersion.uid from next_cursor when continuing a truncated dependency page.",
					},
					"after_dependency_id": map[string]any{
						"type":        "string",
						"description": "PackageDependency.uid from next_cursor when continuing a truncated dependency page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum dependency edges to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "list_package_registry_correlations",
			Description: "List reducer-owned package ownership candidates, publication evidence, and manifest-backed consumption correlations by package_id or repository_id. A correlation persists only after the opt-in package_registry collector populates registry identities to correlate against (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus registry credentials), so a default git-only deploy returns an empty page.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package_id": map[string]any{
						"type":        "string",
						"description": "Package.uid to anchor package ownership, publication, or consumption correlation lookup.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Repository.id to anchor package ownership, publication, or consumption correlation lookup.",
					},
					"relationship_kind": map[string]any{
						"type":        "string",
						"description": "Optional relationship kind filter.",
						"enum":        []string{"ownership", "publication", "consumption"},
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

// repositoryManifestDependencyTools defines the bounded repository
// manifest-dependency tool. It lives in the package-registry family file
// because its subject is package dependencies and its route is claimed by
// the package-registry leaf, but it registers last in ReadOnlyTools so no
// earlier tool's registration position moves (see the position pins the
// tools/list order tests hold).
func repositoryManifestDependencyTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_repository_manifest_dependencies",
			Description: "List a repository's declared manifest dependencies: package name, declared version or range, manifest file, ecosystem, and the dev-or-runtime signal when the fact says so. Reads the active-generation git content-entity facts, so the rows are declared, not resolved, and no repository ownership beyond the scope anchor is asserted. A granted repository with no manifest, or an ungranted or unknown repository, returns the same empty list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Repository scope anchor. Only a granted repository returns rows.",
					},
					"after_observed_at": map[string]any{
						"type":        "string",
						"description": "Keyset cursor observed_at component from next_cursor when continuing a truncated page. Must be sent with after_fact_id.",
					},
					"after_fact_id": map[string]any{
						"type":        "string",
						"description": "Keyset cursor fact_id component from next_cursor when continuing a truncated page. Must be sent with after_observed_at.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum dependency rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"repository_id"},
			},
		},
	}
}
