package mcp

func importDependencyTool() ToolDefinition {
	return ToolDefinition{
		Name:        "investigate_import_dependencies",
		Description: "Investigate bounded import and module dependency questions such as imports by file, importers, package imports, circular Python file imports, and cross-module calls. Provide at least one scope filter: repo_id, source_file, target_file, source_module, or target_module.",
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
					"description": "Optional language filter. file_import_cycles currently supports python.",
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
