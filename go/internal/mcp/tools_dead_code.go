package mcp

func deadCodeInvestigationTool() ToolDefinition {
	return ToolDefinition{
		Name:        "investigate_dead_code",
		Description: "Investigate dead-code candidates with coverage, language maturity, exactness blockers, candidate buckets, source handles, and conservative ambiguity for JavaScript/TypeScript precision risk.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional repository selector: canonical ID, repository name, repo slug, or indexed path",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional parser language filter such as go, python, typescript, tsx, javascript, java, rust, c, cpp, csharp, or sql",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum active dead-code candidates to return after policy filtering",
					"default":     100,
					"maximum":     500,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based offset across active candidates for paging",
					"default":     0,
					"maximum":     2000,
				},
				"exclude_decorated_with": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Decorator names to suppress from returned active candidates",
					"default":     []any{},
				},
			},
			"required": []string{},
		},
	}
}
