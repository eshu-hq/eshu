// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergencetools

import (
	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Tools returns the two MCP code-divergence tool definitions: the
// repo-scoped parallel-implementation findings report and the single-finding
// investigation with bounded follow-up calls. Both accept the drifted family
// alongside exact and renamed, plus the graph-qualified wrapper_bypass and
// convention_outlier families.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		findCodeDivergenceTool(),
		investigateCodeDivergenceTool(),
	}
}

func findCodeDivergenceTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "find_code_divergence",
		Description: "Find parallel implementations in one repository: functions with identical token streams (exact), identical streams up to renaming (renamed), reducer-verified near-duplicate pairs (drifted), targets fronted by a canonical wrapper with cross-package direct callers (wrapper_bypass), or cohort members missing a call the cohort majority makes (convention_outlier), ranked members x tokens with reasons that sum to the score. Suppressions are counted per rule, never silent; truth level is derived. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Canonical repository identifier; required and resolved against the caller's grant",
				},
				"kind": map[string]any{
					"type":        "string",
					"description": "Family: exact, renamed, drifted, wrapper_bypass, convention_outlier, or blank for all five",
					"enum":        []string{"", "exact", "renamed", "drifted", "wrapper_bypass", "convention_outlier"},
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum findings to return",
					"default":     25,
					"minimum":     1,
					"maximum":     100,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based result offset for paging findings",
					"default":     0,
					"minimum":     0,
					"maximum":     10000,
				},
				"include_tests": map[string]any{
					"type":        "boolean",
					"description": "Opt test-file copies back into the member set for the exact, renamed, and convention_outlier families; test files suppress by default. Drifted pairs touching test files are dropped at write, so include_tests has no effect on drifted findings",
					"default":     false,
				},
			},
			"required": []string{"repo_id"},
		},
	}
}

func investigateCodeDivergenceTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "investigate_code_divergence",
		Description: "Drill into one divergence finding addressed by kind and fingerprint: the same member shape as the findings report plus bounded follow-up calls with arguments filled in (call chain and file range per member). Truth level is derived. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Canonical repository identifier; required and resolved against the caller's grant",
				},
				"kind": map[string]any{
					"type":        "string",
					"description": "Family the fingerprint belongs to; accepts the short (exact, renamed, drifted, wrapper_bypass, convention_outlier) and qualified (parallel_implementation.*) spellings; wrapper_bypass fingerprints carry the target entity id, convention_outlier fingerprints carry the cohort address plus the majority callee",
					"enum":        []string{"exact", "renamed", "drifted", "wrapper_bypass", "convention_outlier", "parallel_implementation.exact", "parallel_implementation.renamed", "parallel_implementation.drifted", "parallel_implementation.wrapper_bypass", "parallel_implementation.convention_outlier"},
				},
				"fingerprint": map[string]any{
					"type":        "string",
					"description": "Finding fingerprint from a findings report entry",
				},
				"include_tests": map[string]any{
					"type":        "boolean",
					"description": "Opt test-file copies back into the member set for the exact, renamed, and convention_outlier families; test files suppress by default. Drifted pairs touching test files are dropped at write, so include_tests has no effect on drifted findings",
					"default":     false,
				},
			},
			"required": []string{"repo_id", "kind", "fingerprint"},
		},
	}
}
