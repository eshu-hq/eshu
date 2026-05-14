package mcp

import (
	"strconv"
	"strings"
)

func str(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intOr(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

func boolOr(args map[string]any, key string, def bool) bool {
	v, ok := args[key].(bool)
	if !ok {
		return def
	}
	return v
}

func resolveEntityBody(args map[string]any) map[string]any {
	body := map[string]any{"limit": intOr(args, "limit", 10)}

	if name := str(args, "name"); name != "" {
		body["name"] = name
	} else if query := str(args, "query"); query != "" {
		body["name"] = query
	}
	if kind := str(args, "type"); kind != "" {
		body["type"] = kind
	} else if kinds := stringSlice(args, "types"); len(kinds) > 0 {
		body["type"] = firstString(kinds)
	}
	if repoID := str(args, "repo_id"); repoID != "" {
		body["repo_id"] = repoID
	}

	return body
}

func contentSearchBody(args map[string]any) map[string]any {
	body := map[string]any{
		"query":  str(args, "query"),
		"limit":  intOr(args, "limit", 10),
		"offset": intOr(args, "offset", 0),
	}
	if body["query"] == "" {
		body["query"] = str(args, "pattern")
	}

	repoIDs := stringSlice(args, "repo_ids")
	switch len(repoIDs) {
	case 0:
		if repoID := str(args, "repo_id"); repoID != "" {
			body["repo_id"] = repoID
		}
	case 1:
		if repoID := firstString(repoIDs); repoID != "" {
			body["repo_id"] = repoID
		}
	default:
		body["repo_ids"] = repoIDs
	}

	return body
}

func parseMaxDepth(args map[string]any, defaultDepth int) int {
	if depth, ok := args["max_depth"].(float64); ok {
		return int(depth)
	}
	contextValue := str(args, "context")
	if contextValue == "" {
		return defaultDepth
	}
	depth, err := strconv.Atoi(strings.TrimSpace(contextValue))
	if err != nil {
		return defaultDepth
	}
	return depth
}

func paginationQuery(args map[string]any, defaultLimit int) map[string]string {
	return map[string]string{
		"limit":  strconv.Itoa(intOr(args, "limit", defaultLimit)),
		"offset": strconv.Itoa(intOr(args, "offset", 0)),
	}
}
