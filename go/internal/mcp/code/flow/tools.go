// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeflowtools

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Tools returns the four MCP code-flow tool definitions: bounded taint-path
// evidence, reaching-definition summaries, control-flow-graph summaries, and
// program-dependence summaries for one repository.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		codeFlowTool(
			"dispatch_taint_path",
			"Inspect bounded taint-path evidence for one repository, labeled as derived reducer evidence when present and partial when absent or ambiguous.",
		),
		codeFlowTool(
			"dispatch_reaching_def",
			"Inspect bounded reaching-definition summaries for one repository from exact parser-emitted data-flow facts.",
		),
		codeFlowTool(
			"dispatch_cfg_summary",
			"Inspect bounded control-flow graph summaries for one repository from exact parser-emitted data-flow facts.",
		),
		codeFlowTool(
			"dispatch_pdg_summary",
			"Inspect bounded program-dependence summaries for one repository, labeled as partial derived summaries when control and def-use facts are combined.",
		),
	}
}

func codeFlowTool(name string, description string) toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        name,
		Description: description,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"repo_id"},
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Canonical repository identifier",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional supported language filter",
				},
				"symbol": map[string]any{
					"type":        "string",
					"description": "Optional function or source/sink symbol filter",
				},
				"file_path": map[string]any{
					"type":        "string",
					"description": "Optional repository-relative file path filter",
				},
				"line": map[string]any{
					"type":        "integer",
					"description": "Optional 1-based line filter",
					"minimum":     1,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum rows to return",
					"default":     25,
					"minimum":     1,
					"maximum":     100,
				},
			},
		},
	}
}
