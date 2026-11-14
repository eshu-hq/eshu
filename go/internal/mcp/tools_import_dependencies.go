// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func importDependencyTool() ToolDefinition {
	return ToolDefinition{
		Name:        "investigate_import_dependencies",
		Description: "Investigate bounded import and module dependency questions such as imports by file, importers, package imports, bounded simple Python file-import cycles, Go package-import cycles, TS/JS package-import cycles, and cross-module calls. Cycles enumerate rotation-deduplicated simple cycles up to max_cycle_length (default 5) after excluding type-only and deferred IMPORTS edges, labelling each cycle runtime, ambiguous (an inferred edge), or flags_unknown (an edge written before the import flags existed); enumeration stops at 1,000 cycles or a fixed step budget and reports truncated:true with the stop reason; page on has_more and next_offset, since truncated stays true on every page of a stopped run. Go cycles run at package level over the projector's file-to-package mapping: pass language=go, and read the file and module slots as package import paths. TS/JS cycles run at package level over the projector's file-to-package mapping: pass language=typescript, tsx, or javascript, and read the file and module slots as npm package names. Provide at least one scope filter: repo_id, source_file, target_file, source_module, target_module, or target_module_prefix. target_file applies only to cycle and cross-module queries. target_module_prefix matches Module names by prefix (STARTS WITH on the module-name index, e.g. @scope/ or github.com/acme/); it is mutually exclusive with target_module and an explicitly empty prefix is rejected. external_only drops relative (./x, ../y) and absolute (/abs) specifiers, keeping bare package names; it applies only to the module-matching reads. Candidate scans above 25,000 rows fail with an instruction to narrow scope. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected.",
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
					"description": "Optional language filter. file_import_cycles supports python multi-node cycle detection, go package cycle detection, typescript/tsx/javascript package cycle detection, and java package cycle detection; other languages are rejected.",
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
				"target_module_prefix": map[string]any{
					"type":        "string",
					"description": "Optional module-name prefix anchor (STARTS WITH, e.g. @scope/ or github.com/acme/); mutually exclusive with target_module, rejected when empty, counts as a scope filter",
				},
				"external_only": map[string]any{
					"type":        "boolean",
					"description": "Exclude relative (./x, ../y) and absolute (/abs) specifiers, keeping bare package names; applies only to the module-matching reads",
					"default":     false,
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

// importDependencyBody builds the POST body for the imports/investigate
// route from tool arguments. The prefix passes through by presence: an
// omitted argument sends no key (no filter), while an explicitly empty one
// sends "" so the handler rejects it instead of silently ignoring it.
func importDependencyBody(args map[string]any) map[string]any {
	body := map[string]any{
		"query_type":       str(args, "query_type"),
		"repo_id":          str(args, "repo_id"),
		"language":         str(args, "language"),
		"source_file":      str(args, "source_file"),
		"target_file":      str(args, "target_file"),
		"source_module":    str(args, "source_module"),
		"target_module":    str(args, "target_module"),
		"external_only":    boolOr(args, "external_only", false),
		"limit":            intOr(args, "limit", 25),
		"offset":           intOr(args, "offset", 0),
		"max_cycle_length": intOr(args, "max_cycle_length", 0),
	}
	if prefix, ok := args["target_module_prefix"].(string); ok {
		body["target_module_prefix"] = prefix
	}
	return body
}
