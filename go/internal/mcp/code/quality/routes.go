// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequalitytools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for a complexity/quality tool
// without executing it. It reports handled only for the three tools this
// package owns. Family membership is an explicit name switch, never a prefix
// match: inspect_code_inventory shares the inspect_code_ spelling but belongs
// to the structural-inventory family that stays in the root switch.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "calculate_cyclomatic_complexity":
		body := map[string]any{
			"function_name": args.String("function_name"),
			"repo_id":       args.String("repo_id"),
		}
		if entityID := args.String("entity_id"); entityID != "" {
			body["entity_id"] = entityID
		}
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/complexity", Body: body}, true
	case "find_most_complex_functions":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/complexity", Body: map[string]any{
			"repo_id": args.String("repo_id"), "limit": args.IntOr("limit", 10),
		}}, true
	case "inspect_code_quality":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/quality/inspect", Body: map[string]any{
			"check":          args.String("check"),
			"repo_id":        args.String("repo_id"),
			"language":       args.String("language"),
			"entity_id":      args.String("entity_id"),
			"function_name":  args.String("function_name"),
			"min_complexity": args.IntOr("min_complexity", 0),
			"min_lines":      args.IntOr("min_lines", 0),
			"min_arguments":  args.IntOr("min_arguments", 0),
			"limit":          args.IntOr("limit", 10),
			"offset":         args.IntOr("offset", 0),
		}}, true
	default:
		return routecontract.Request{}, false
	}
}

// The three bodies preserve the exact wire shape the root switch arms sent.
//
// Both complexity tools share POST /api/v0/code/complexity: the handler
// branches on the selectors, answering a single-function lookup when
// entity_id or function_name is non-empty and the most-complex list
// otherwise. calculate_cyclomatic_complexity sends entity_id only when the
// caller supplied a non-empty string — the key's absence, not just its
// emptiness, is the pinned wire shape — and deliberately sends no limit key,
// so a caller whose selectors are both blank falls through to the list mode
// at the handler's own default page of 10. Its advertised schema also names
// path and scope; neither is selected here and the handler decodes neither,
// an inherited advertised-versus-dispatched asymmetry, not a dropped field.
//
// find_most_complex_functions and inspect_code_quality default limit to 10,
// the same value both handlers substitute for a nonpositive limit before
// clamping anything above 100 down to 100 (normalizeComplexityListLimit in
// query's code_complexity_page.go and normalizeCodeQualityLimit in
// code_quality.go), so the dispatcher's default is indistinguishable from an
// omitted limit and no limit value can 400.
//
// inspect_code_quality's offset is the one argument whose two bounds act in
// opposite directions: the handler floors a negative offset to 0 but rejects
// anything above 10000 with HTTP 400 ("offset exceeds maximum"). The three
// min_* thresholds travel as 0 when omitted so the handler resolves its own
// check-specific defaults (min_lines 20, min_arguments 5, min_complexity 1
// for the complexity check and 10 otherwise); forwarding a positive default
// here would pin one check's threshold onto every other check. A blank check
// resolves to refactoring_candidates at the handler, while an unsupported
// non-blank check rejects with HTTP 400.
