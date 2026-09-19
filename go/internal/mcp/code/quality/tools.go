// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequalitytools

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Tools returns the three MCP complexity/quality tool definitions: the
// single-function cyclomatic complexity lookup, the most-complex-functions
// ranking, and the bounded code-quality inspection.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		cyclomaticComplexityTool(),
		mostComplexFunctionsTool(),
		codeQualityInspectionTool(),
	}
}

func cyclomaticComplexityTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "calculate_cyclomatic_complexity",
		Description: "Calculate the cyclomatic complexity of a specific function to measure its complexity. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity_id": map[string]any{
					"type":        "string",
					"description": "Exact entity identifier returned by an ambiguity response",
				},
				"function_name": map[string]any{
					"type":        "string",
					"description": "Name of the function to analyze when entity_id is unknown",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional file path containing the function",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier",
				},
				"scope": map[string]any{
					"type":        "string",
					"description": "Analysis scope",
					"default":     "auto",
				},
			},
			"required": []string{},
		},
	}
}

func mostComplexFunctionsTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "find_most_complex_functions",
		Description: "Find the most complex functions in the codebase based on cyclomatic complexity. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return",
					"default":     10,
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier",
				},
			},
			"required": []string{},
		},
	}
}

func codeQualityInspectionTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "inspect_code_quality",
		Description: "Inspect bounded code-quality and refactoring metrics for functions: complexity, function length, argument count, or combined refactoring candidates. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"check": map[string]any{
					"type":        "string",
					"description": "Metric family to inspect",
					"enum":        []string{"complexity", "function_length", "argument_count", "refactoring_candidates"},
					"default":     "refactoring_candidates",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier to scope the inspection",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter",
				},
				"entity_id": map[string]any{
					"type":        "string",
					"description": "Optional exact function entity identifier",
				},
				"function_name": map[string]any{
					"type":        "string",
					"description": "Optional exact function name",
				},
				"min_complexity": map[string]any{
					"type":        "integer",
					"description": "Minimum cyclomatic complexity. When omitted or <= 0, the server uses 1 for complexity ranking and 10 for refactoring candidates.",
				},
				"min_lines": map[string]any{
					"type":        "integer",
					"description": "Minimum function line count for length/refactoring checks",
					"default":     20,
				},
				"min_arguments": map[string]any{
					"type":        "integer",
					"description": "Minimum function argument count for argument/refactoring checks",
					"default":     5,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum functions to return",
					"default":     10,
					"minimum":     1,
					"maximum":     100,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based result offset for paging",
					"default":     0,
					"minimum":     0,
					"maximum":     10000,
				},
			},
			"required": []string{},
		},
	}
}
