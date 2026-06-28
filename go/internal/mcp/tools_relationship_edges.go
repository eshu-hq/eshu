// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "github.com/eshu-hq/eshu/go/internal/sourcetool"

// relationshipEdgesTool defines the bounded relationship-edges read tool. It
// proxies POST /api/v0/relationships/edges, which returns a slice of concrete
// typed edges for one verb from the canonical graph. The optional source_tool
// filter is validated against the closed sourcetool.Canonical vocabulary before
// the request is forwarded; an unknown value is rejected with a clear error.
func relationshipEdgesTool() ToolDefinition {
	return ToolDefinition{
		Name: "list_relationship_edges",
		Description: "List a bounded slice of concrete typed graph edges for one relationship verb, " +
			"each with source/target endpoints, evidence, and optional source_tool label. " +
			"verb is required; source_tool narrows results to edges stamped by that tool " +
			"(Tier-2 cross-repo verbs only — Tier-1 self-labeling and Tier-3 code verbs " +
			"always return an empty slice for any source_tool filter). " +
			"limit defaults to 50 and is capped at 200. " +
			"truncated is set when more edges exist beyond the returned slice.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"verb": map[string]any{
					"type":        "string",
					"description": "Relationship verb from the catalog (e.g. DEPENDS_ON, DEPLOYS_FROM, USES_MODULE). Case-insensitive; normalized to upper-case.",
				},
				"source_tool": map[string]any{
					"type":        "string",
					"description": "Optional source-tool filter. Restricts edges to those stamped with this tool token. Must be a value from the canonical vocabulary; an unknown value is rejected. Empty means no filter.",
					"enum":        sourcetoolEnumAny(),
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum edges to return. Defaults to 50, capped at 200.",
					"default":     50,
					"minimum":     1,
					"maximum":     200,
				},
			},
			"required": []string{"verb"},
		},
	}
}

// sourcetoolEnumAny converts sourcetool.Canonical to []any for use in JSON
// schema enum fields, which the MCP transport expects as []any.
func sourcetoolEnumAny() []any {
	values := make([]any, len(sourcetool.Canonical))
	for i, v := range sourcetool.Canonical {
		values[i] = v
	}
	return values
}
