// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecosystemtools

import "github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"

func packageRegistryDefinitionTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "list_package_registry_packages",
			Description: "List package registry package identities by package_id or ecosystem/name without inferring repository ownership; malformed rows are returned under identity_issues. Populated by the opt-in package_registry collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus registry credentials), so a default git-only deploy returns an empty page.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package_id": map[string]any{
						"type":        "string",
						"description": "Exact Package.uid lookup.",
					},
					"ecosystem": map[string]any{
						"type":        "string",
						"description": "Package ecosystem scope such as npm, maven, pypi, go, cargo, hex, or nuget.",
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
			Description: "List package registry version identities for one Package.uid without inferring repository ownership. Populated by the opt-in package_registry collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus registry credentials), so a default git-only deploy returns an empty page.",
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
	}
}
