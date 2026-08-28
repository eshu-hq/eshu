// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// codeRelationshipRoute claims the code-relationship family while leaving
// unrelated tools available to the remaining dispatch fanout.
func codeRelationshipRoute(toolName string, args map[string]any) (*route, bool, error) {
	request, handled, err := codeRelationshipRequest(toolName, routecontract.Arguments(args))
	if !handled || err != nil {
		return nil, handled, err
	}
	return &route{
		method: request.Method,
		path:   request.Path,
		body:   request.Body,
		query:  request.Query,
	}, true, nil
}

// codeRelationshipRequest selects a dependency-neutral request while the root
// package retains route membership and dispatch ownership.
func codeRelationshipRequest(toolName string, args routecontract.Arguments) (routecontract.Request, bool, error) {
	switch toolName {
	case "get_code_relationship_story":
		return codeRelationshipStoryRequest(args), true, nil
	case "analyze_code_relationships":
		request, err := resolveAnalyzeCodeRelationshipsRequest(args)
		return request, true, err
	default:
		return routecontract.Request{}, false, nil
	}
}

func codeRelationshipStoryRequest(args routecontract.Arguments) routecontract.Request {
	body := map[string]any{
		"target":             args.String("target"),
		"entity_id":          args.String("entity_id"),
		"repo_id":            args.String("repo_id"),
		"language":           args.String("language"),
		"relationship_type":  args.String("relationship_type"),
		"relationship_types": args.StringSlice("relationship_types"),
		"direction":          args.String("direction"),
		"include_transitive": args.BoolOr("include_transitive", false),
		"max_depth":          args.IntOr("max_depth", 5),
		"limit":              args.IntOr("limit", 25),
		"offset":             args.IntOr("offset", 0),
		"token_budget":       args.IntOr("token_budget", 0),
		"cross_repo":         analyzeCodeRelationshipsCrossRepo(args, false),
	}
	if minConfidence, ok := args.OptionalFloat("min_confidence"); ok {
		body["min_confidence"] = minConfidence
	}
	return routecontract.Request{Method: "POST", Path: "/api/v0/code/relationships/story", Body: body}
}

// resolveAnalyzeCodeRelationshipsRequest maps an analyze_code_relationships call
// to the bounded HTTP request for its query_type. Direct caller/callee/importer
// queries flow through the relationship story route (and carry the additive
// token_budget and relationship_types filters); typed and path queries use their
// dedicated routes.
func resolveAnalyzeCodeRelationshipsRequest(args routecontract.Arguments) (routecontract.Request, error) {
	switch args.String("query_type") {
	case "find_callers":
		return analyzeCodeRelationshipsStoryRequest(args, "incoming", "CALLS", false), nil
	case "find_callees":
		return analyzeCodeRelationshipsStoryRequest(args, "outgoing", "CALLS", false), nil
	case "find_all_callers":
		return analyzeCodeRelationshipsStoryRequest(args, "incoming", "CALLS", true), nil
	case "find_all_callees":
		return analyzeCodeRelationshipsStoryRequest(args, "outgoing", "CALLS", true), nil
	case "find_cross_repo_callers":
		return analyzeCodeRelationshipsStoryRequest(args, "incoming", "CALLS", false, true), nil
	case "find_cross_repo_callees":
		return analyzeCodeRelationshipsStoryRequest(args, "outgoing", "CALLS", false, true), nil
	case "find_importers":
		return analyzeCodeRelationshipsStoryRequest(args, "incoming", "IMPORTS", false), nil
	case "find_cross_repo_importers":
		return analyzeCodeRelationshipsStoryRequest(args, "incoming", "IMPORTS", false, true), nil
	case "class_hierarchy":
		return analyzeCodeRelationshipsTypedStoryRequest(args, "class_hierarchy", "both", "INHERITS"), nil
	case "cross_repo_class_hierarchy":
		return analyzeCodeRelationshipsStoryRequest(args, "both", "INHERITS", false, true), nil
	case "overrides":
		return analyzeCodeRelationshipsTypedStoryRequest(args, "overrides", "both", "OVERRIDES"), nil
	case "cross_repo_overrides":
		return analyzeCodeRelationshipsStoryRequest(args, "both", "OVERRIDES", false, true), nil
	case "call_chain", "find_cross_repo_call_chain":
		startEntityID := args.String("start_entity_id")
		endEntityID := args.String("end_entity_id")
		start, end := "", ""
		if target := args.String("target"); target != "" {
			var ok bool
			start, end, ok = strings.Cut(target, "->")
			if !ok {
				return routecontract.Request{}, fmt.Errorf("call_chain target must use start->end format")
			}
			start = strings.TrimSpace(start)
			end = strings.TrimSpace(end)
		}
		if start == "" && startEntityID == "" || end == "" && endEntityID == "" {
			return routecontract.Request{}, fmt.Errorf("call_chain target must use start->end format or provide start_entity_id and end_entity_id")
		}
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/call-chain", Body: map[string]any{
			"start":           start,
			"end":             end,
			"repo_id":         args.String("repo_id"),
			"cross_repo":      analyzeCodeRelationshipsCrossRepo(args, args.String("query_type") == "find_cross_repo_call_chain"),
			"start_repo_id":   args.String("start_repo_id"),
			"end_repo_id":     args.String("end_repo_id"),
			"start_entity_id": startEntityID,
			"end_entity_id":   endEntityID,
			"max_depth":       parseCodeRelationshipMaxDepth(args, 5),
		}}, nil
	case "dead_code":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/dead-code", Body: map[string]any{
			"repo_id":                args.String("repo_id"),
			"limit":                  args.IntOr("limit", 100),
			"exclude_decorated_with": args.StringSlice("exclude_decorated_with"),
		}}, nil
	}
	return routecontract.Request{Method: "POST", Path: "/api/v0/code/relationships", Body: map[string]any{
		"entity_id":  args.String("target"),
		"query_type": args.String("query_type"),
	}}, nil
}

func analyzeCodeRelationshipsStoryRequest(
	args routecontract.Arguments,
	direction string,
	relationshipType string,
	includeTransitive bool,
	forceCrossRepo ...bool,
) routecontract.Request {
	body := map[string]any{
		"target":             args.String("target"),
		"repo_id":            args.String("repo_id"),
		"direction":          direction,
		"relationship_type":  relationshipType,
		"relationship_types": args.StringSlice("relationship_types"),
		"include_transitive": includeTransitive,
		"max_depth":          parseCodeRelationshipMaxDepth(args, 5),
		"limit":              args.IntOr("limit", 25),
		"offset":             args.IntOr("offset", 0),
		"token_budget":       args.IntOr("token_budget", 0),
		"cross_repo":         analyzeCodeRelationshipsCrossRepo(args, len(forceCrossRepo) > 0 && forceCrossRepo[0]),
	}
	if minConfidence, ok := args.OptionalFloat("min_confidence"); ok {
		body["min_confidence"] = minConfidence
	}
	return routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/code/relationships/story",
		Body:   body,
	}
}

func analyzeCodeRelationshipsTypedStoryRequest(
	args routecontract.Arguments,
	queryType string,
	direction string,
	relationshipType string,
	forceCrossRepo ...bool,
) routecontract.Request {
	body := map[string]any{
		"query_type":        queryType,
		"target":            args.String("target"),
		"repo_id":           args.String("repo_id"),
		"language":          args.String("language"),
		"direction":         direction,
		"relationship_type": relationshipType,
		"max_depth":         parseCodeRelationshipMaxDepth(args, 5),
		"limit":             args.IntOr("limit", 25),
		"offset":            args.IntOr("offset", 0),
		"token_budget":      args.IntOr("token_budget", 0),
		"cross_repo":        analyzeCodeRelationshipsCrossRepo(args, len(forceCrossRepo) > 0 && forceCrossRepo[0]),
	}
	if minConfidence, ok := args.OptionalFloat("min_confidence"); ok {
		body["min_confidence"] = minConfidence
	}
	return routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/code/relationships/story",
		Body:   body,
	}
}

func analyzeCodeRelationshipsCrossRepo(args routecontract.Arguments, forced bool) bool {
	return forced || args.BoolOr("cross_repo", false) || strings.EqualFold(args.String("scope"), "cross_repo")
}

func parseCodeRelationshipMaxDepth(args routecontract.Arguments, defaultDepth int) int {
	if depth, ok := args["max_depth"].(float64); ok {
		return int(depth)
	}
	if depth, ok := args["max_depth"].(int); ok {
		return depth
	}
	contextValue := args.String("context")
	if contextValue == "" {
		return defaultDepth
	}
	depth, err := strconv.Atoi(strings.TrimSpace(contextValue))
	if err != nil {
		return defaultDepth
	}
	return depth
}
