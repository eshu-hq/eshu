package mcp

func analyzeCodeRelationshipsStoryRoute(
	args map[string]any,
	direction string,
	relationshipType string,
	includeTransitive bool,
) *route {
	return &route{
		method: "POST",
		path:   "/api/v0/code/relationships/story",
		body: map[string]any{
			"target":             str(args, "target"),
			"repo_id":            str(args, "repo_id"),
			"direction":          direction,
			"relationship_type":  relationshipType,
			"include_transitive": includeTransitive,
			"max_depth":          parseMaxDepth(args, 5),
			"limit":              intOr(args, "limit", 25),
			"offset":             intOr(args, "offset", 0),
		},
	}
}
