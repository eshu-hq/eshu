// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contenttools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for a content tool without
// executing it. It reports handled only for the five tools this package
// owns: repo-relative file read, repo-relative line-range read, indexed
// file-content search, cached entity-source-snippet search, and bounded
// evidence-citation hydration. Family membership is an explicit name
// switch, never a prefix match, so a future tool spelled similarly cannot
// be silently absorbed.
//
// get_entity_content is deliberately not part of this family even though
// its registration lives beside these five in tools_content.go: its route
// selection lives in entityresolution, which shares no helper with this
// package, so moving its schema entry here would split a coherent
// registration group without moving any coupled routing.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "get_file_content":
		return routecontract.Request{Method: "POST", Path: "/api/v0/content/files/read", Body: map[string]any{
			"repo_id":       args.String("repo_id"),
			"relative_path": args.String("relative_path"),
		}}, true
	case "get_file_lines":
		// The root arm forwarded args unchanged rather than building a fresh
		// map, so the handler alone owns start_line/end_line/repo_id/
		// relative_path validation; this preserves that exact aliasing
		// behavior instead of copying it into a new map.
		return routecontract.Request{Method: "POST", Path: "/api/v0/content/files/lines", Body: map[string]any(args)}, true
	case "build_evidence_citation_packet":
		return routecontract.Request{Method: "POST", Path: "/api/v0/evidence/citations", Body: map[string]any{
			"subject":  args["subject"],
			"question": args.String("question"),
			"handles":  args["handles"],
			"limit":    args.IntOr("limit", 10),
		}}, true
	case "search_file_content":
		return routecontract.Request{Method: "POST", Path: "/api/v0/content/files/search", Body: contentSearchBody(args)}, true
	case "search_entity_content":
		return routecontract.Request{Method: "POST", Path: "/api/v0/content/entities/search", Body: contentSearchBody(args)}, true
	default:
		return routecontract.Request{}, false
	}
}

// contentSearchBody preserves the exact wire shape the root switch built for
// search_file_content and search_entity_content through the former root
// helper of the same name: query prefers the query argument and falls back
// to pattern when query is blank; repo scope collapses to a single repo_id
// key when zero or one repo selector is supplied and to repo_ids only when
// more than one is supplied, matching the handler's single/multi-repo
// contract. limit defaults to 10 and offset to 0, the same values the
// content-search handlers substitute for an absent argument.
func contentSearchBody(args routecontract.Arguments) map[string]any {
	body := map[string]any{
		"query":  args.String("query"),
		"limit":  args.IntOr("limit", 10),
		"offset": args.IntOr("offset", 0),
	}
	if body["query"] == "" {
		body["query"] = args.String("pattern")
	}

	repoIDs := args.StringSlice("repo_ids")
	switch len(repoIDs) {
	case 0:
		if repoID := args.String("repo_id"); repoID != "" {
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

// firstString mirrors the root dispatcher's helper of the same name: it
// narrows a decoded []any to its first string member, or an empty string
// when the slice is empty or the first element is not a string.
func firstString(values []any) string {
	if len(values) == 0 {
		return ""
	}
	value, _ := values[0].(string)
	return value
}
