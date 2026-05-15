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

func analyzeCodeRelationshipsTypedStoryRoute(
	args map[string]any,
	queryType string,
	direction string,
	relationshipType string,
) *route {
	return &route{
		method: "POST",
		path:   "/api/v0/code/relationships/story",
		body: map[string]any{
			"query_type":        queryType,
			"target":            str(args, "target"),
			"repo_id":           str(args, "repo_id"),
			"language":          str(args, "language"),
			"direction":         direction,
			"relationship_type": relationshipType,
			"max_depth":         parseMaxDepth(args, 5),
			"limit":             intOr(args, "limit", 25),
			"offset":            intOr(args, "offset", 0),
		},
	}
}
