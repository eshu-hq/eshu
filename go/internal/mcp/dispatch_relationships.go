// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"strings"
)

func codeRelationshipStoryRoute(args map[string]any) *route {
	body := map[string]any{
		"target":             str(args, "target"),
		"entity_id":          str(args, "entity_id"),
		"repo_id":            str(args, "repo_id"),
		"language":           str(args, "language"),
		"relationship_type":  str(args, "relationship_type"),
		"relationship_types": stringSlice(args, "relationship_types"),
		"direction":          str(args, "direction"),
		"include_transitive": boolOr(args, "include_transitive", false),
		"max_depth":          intOr(args, "max_depth", 5),
		"limit":              intOr(args, "limit", 25),
		"offset":             intOr(args, "offset", 0),
		"token_budget":       intOr(args, "token_budget", 0),
		"cross_repo":         analyzeCodeRelationshipsCrossRepo(args, false),
	}
	if minConfidence, ok := optionalFloat(args, "min_confidence"); ok {
		body["min_confidence"] = minConfidence
	}
	return &route{method: "POST", path: "/api/v0/code/relationships/story", body: body}
}

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
	case "find_cross_repo_callers":
		return analyzeCodeRelationshipsStoryRoute(args, "incoming", "CALLS", false, true), nil
	case "find_cross_repo_callees":
		return analyzeCodeRelationshipsStoryRoute(args, "outgoing", "CALLS", false, true), nil
	case "find_importers":
		return analyzeCodeRelationshipsStoryRoute(args, "incoming", "IMPORTS", false), nil
	case "find_cross_repo_importers":
		return analyzeCodeRelationshipsStoryRoute(args, "incoming", "IMPORTS", false, true), nil
	case "class_hierarchy":
		return analyzeCodeRelationshipsTypedStoryRoute(args, "class_hierarchy", "both", "INHERITS"), nil
	case "cross_repo_class_hierarchy":
		return analyzeCodeRelationshipsStoryRoute(args, "both", "INHERITS", false, true), nil
	case "overrides":
		return analyzeCodeRelationshipsTypedStoryRoute(args, "overrides", "both", "OVERRIDES"), nil
	case "cross_repo_overrides":
		return analyzeCodeRelationshipsStoryRoute(args, "both", "OVERRIDES", false, true), nil
	case "call_chain", "find_cross_repo_call_chain":
		startEntityID := str(args, "start_entity_id")
		endEntityID := str(args, "end_entity_id")
		start, end := "", ""
		if target := str(args, "target"); target != "" {
			var ok bool
			start, end, ok = strings.Cut(target, "->")
			if !ok {
				return nil, fmt.Errorf("call_chain target must use start->end format")
			}
			start = strings.TrimSpace(start)
			end = strings.TrimSpace(end)
		}
		if start == "" && startEntityID == "" || end == "" && endEntityID == "" {
			return nil, fmt.Errorf("call_chain target must use start->end format or provide start_entity_id and end_entity_id")
		}
		return &route{method: "POST", path: "/api/v0/code/call-chain", body: map[string]any{
			"start":           start,
			"end":             end,
			"repo_id":         str(args, "repo_id"),
			"cross_repo":      analyzeCodeRelationshipsCrossRepo(args, str(args, "query_type") == "find_cross_repo_call_chain"),
			"start_repo_id":   str(args, "start_repo_id"),
			"end_repo_id":     str(args, "end_repo_id"),
			"start_entity_id": startEntityID,
			"end_entity_id":   endEntityID,
			"max_depth":       parseMaxDepth(args, 5),
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
	forceCrossRepo ...bool,
) *route {
	body := map[string]any{
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
		"cross_repo":         analyzeCodeRelationshipsCrossRepo(args, len(forceCrossRepo) > 0 && forceCrossRepo[0]),
	}
	if minConfidence, ok := optionalFloat(args, "min_confidence"); ok {
		body["min_confidence"] = minConfidence
	}
	return &route{
		method: "POST",
		path:   "/api/v0/code/relationships/story",
		body:   body,
	}
}

func analyzeCodeRelationshipsTypedStoryRoute(
	args map[string]any,
	queryType string,
	direction string,
	relationshipType string,
	forceCrossRepo ...bool,
) *route {
	body := map[string]any{
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
		"cross_repo":        analyzeCodeRelationshipsCrossRepo(args, len(forceCrossRepo) > 0 && forceCrossRepo[0]),
	}
	if minConfidence, ok := optionalFloat(args, "min_confidence"); ok {
		body["min_confidence"] = minConfidence
	}
	return &route{
		method: "POST",
		path:   "/api/v0/code/relationships/story",
		body:   body,
	}
}

func analyzeCodeRelationshipsCrossRepo(args map[string]any, forced bool) bool {
	return forced || boolOr(args, "cross_repo", false) || strings.EqualFold(str(args, "scope"), "cross_repo")
}
