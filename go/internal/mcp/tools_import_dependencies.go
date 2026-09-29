// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func importDependencyTool() ToolDefinition {
	return ToolDefinition{
		Name:        "investigate_import_dependencies",
		Description: "Investigate bounded import and module dependency questions such as imports by file, importers, package imports, bounded simple Python file-import cycles, and cross-module calls. Cycles enumerate rotation-deduplicated simple cycles up to max_cycle_length (default 5) over all stored IMPORTS edges with no type-only or deferred exclusion; enumeration stops at 1,000 cycles or a fixed step budget and reports truncated:true with the stop reason; page on has_more and next_offset, since truncated stays true on every page of a stopped run. Provide at least one scope filter: repo_id, source_file, target_file, source_module, or target_module. target_file applies only to cycle and cross-module queries. Candidate scans above 25,000 rows fail with an instruction to narrow scope. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query_type": map[string]any{
					"type":        "string",
					"description": "Import/dependency investigation shape",
					"enum":        []string{"imports_by_file", "importers", "module_dependencies", "package_imports", "file_import_cycles", "cross_module_calls"},
					"default":     "imports_by_file",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional canonical repository identifier",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter. file_import_cycles supports python multi-node cycle detection; other languages are rejected.",
				},
				"max_cycle_length": map[string]any{
					"type":        "integer",
					"description": "Simple-cycle length bound for file_import_cycles (default 5). Ignored by other query types.",
					"default":     5,
					"minimum":     2,
					"maximum":     8,
				},
				"source_file": map[string]any{
					"type":        "string",
					"description": "Optional repo-relative source file path anchor",
				},
				"target_file": map[string]any{
					"type":        "string",
					"description": "Optional repo-relative target file path for cross-module call and cycle queries",
				},
				"source_module": map[string]any{
					"type":        "string",
					"description": "Optional source module name anchor",
				},
				"target_module": map[string]any{
					"type":        "string",
					"description": "Optional imported or target module name anchor",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum dependency rows to return",
					"default":     25,
					"minimum":     0,
					"maximum":     200,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based result offset for paging",
					"default":     0,
					"minimum":     0,
					"maximum":     10000,
				},
			},
		},
	}
}
