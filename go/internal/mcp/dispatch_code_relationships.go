package mcp

import (
	"fmt"
	"strings"
)

func resolveAnalyzeCodeRelationshipsRoute(args map[string]any) (*route, error) {
	body := map[string]any{
		"entity_id":  str(args, "target"),
		"query_type": str(args, "query_type"),
	}
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
	case "find_implementers":
		return analyzeCodeRelationshipsStoryRoute(args, "incoming", "IMPLEMENTS", false), nil
	case "find_implementations":
		return analyzeCodeRelationshipsStoryRoute(args, "outgoing", "IMPLEMENTS", false), nil
	case "find_instantiators":
		return analyzeCodeRelationshipsStoryRoute(args, "incoming", "INSTANTIATES", false), nil
	case "find_instantiations":
		return analyzeCodeRelationshipsStoryRoute(args, "outgoing", "INSTANTIATES", false), nil
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
	default:
		return &route{method: "POST", path: "/api/v0/code/relationships", body: body}, nil
	}
}
