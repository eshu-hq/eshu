package mcp

import (
	"fmt"
	"strings"
)

// resolveAnalyzeCodeRelationshipsRoute maps an analyze_code_relationships call to
// the bounded HTTP route for its query_type. Direct caller/callee/importer
// queries flow through the relationship story route (and carry the additive
// token_budget and relationship_types filters); typed and path queries use their
// dedicated routes.
func resolveAnalyzeCodeRelationshipsRoute(args map[string]any) (*route, error) {
	switch str(args, "query_type") {
	case "find_callers":
		return analyzeCodeRelationshipsStoryRoute(args, "incoming", "CALLS", false), nil
	case "find_callees":
		return analyzeCodeRelationshipsStoryRoute(args, "outgoing", "CALLS", false), nil
	case "find_all_callers":
		return analyzeCodeRelationshipsStoryRoute(args, "incoming", "CALLS", true), nil
	case "find_all_callees":
		return analyzeCodeRelationshipsStoryRoute(args, "outgoing", "CALLS", true), nil
	case "find_importers":
		return analyzeCodeRelationshipsStoryRoute(args, "incoming", "IMPORTS", false), nil
	case "class_hierarchy":
		return analyzeCodeRelationshipsTypedStoryRoute(args, "class_hierarchy", "both", "INHERITS"), nil
	case "overrides":
		return analyzeCodeRelationshipsTypedStoryRoute(args, "overrides", "both", "OVERRIDES"), nil
	case "call_chain":
		start, end, ok := strings.Cut(str(args, "target"), "->")
		if !ok {
			return nil, fmt.Errorf("call_chain target must use start->end format")
		}
		return &route{method: "POST", path: "/api/v0/code/call-chain", body: map[string]any{
			"start":     strings.TrimSpace(start),
			"end":       strings.TrimSpace(end),
			"max_depth": parseMaxDepth(args, 5),
		}}, nil
	case "dead_code":
		return &route{method: "POST", path: "/api/v0/code/dead-code", body: map[string]any{
			"repo_id":                str(args, "repo_id"),
			"limit":                  intOr(args, "limit", 100),
			"exclude_decorated_with": stringSlice(args, "exclude_decorated_with"),
		}}, nil
	}
	return &route{method: "POST", path: "/api/v0/code/relationships", body: map[string]any{
		"entity_id":  str(args, "target"),
		"query_type": str(args, "query_type"),
	}}, nil
}

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
			"relationship_types": stringSlice(args, "relationship_types"),
			"include_transitive": includeTransitive,
			"max_depth":          parseMaxDepth(args, 5),
			"limit":              intOr(args, "limit", 25),
			"offset":             intOr(args, "offset", 0),
			"token_budget":       intOr(args, "token_budget", 0),
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
			"token_budget":      intOr(args, "token_budget", 0),
		},
	}
}
