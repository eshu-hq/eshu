// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinteltools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for a code-intelligence tool
// without executing it. It reports handled only for the eight tools this
// package owns: entity-name and symbol search, structural inventory, call
// graph metrics, route-caller tracing, topic investigation, the
// language-specific AST query, and the function call-chain finder. Family
// membership is an explicit name switch, never a prefix match, so a future
// tool spelled similarly cannot be silently absorbed.
//
// search_entity_content and search_file_content are not part of this family:
// both build their body from the shared contentSearchBody helper and live
// together in the content child package instead, so splitting one out from
// that pair does not orphan the shared helper from the family that owns it.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "find_code":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/search", Body: map[string]any{
			"query":    args.String("query"),
			"repo_id":  args.String("repo_id"),
			"language": args.String("language"),
			"limit":    args.IntOr("limit", 10),
			"exact":    args.BoolOr("exact", false),
		}}, true
	case "find_symbol":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/symbols/search", Body: map[string]any{
			"symbol":       args.String("symbol"),
			"match_mode":   args.String("match_mode"),
			"repo_id":      args.String("repo_id"),
			"language":     args.String("language"),
			"entity_type":  args.String("entity_type"),
			"entity_types": args.StringSlice("entity_types"),
			"limit":        args.IntOr("limit", 25),
			"offset":       args.IntOr("offset", 0),
		}}, true
	case "inspect_code_inventory":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/structure/inventory", Body: map[string]any{
			"repo_id":        args.String("repo_id"),
			"language":       args.String("language"),
			"inventory_kind": args.String("inventory_kind"),
			"entity_kind":    args.String("entity_kind"),
			"file_path":      args.String("file_path"),
			"symbol":         args.String("symbol"),
			"decorator":      args.String("decorator"),
			"method_name":    args.String("method_name"),
			"class_name":     args.String("class_name"),
			"limit":          args.IntOr("limit", 25),
			"offset":         args.IntOr("offset", 0),
		}}, true
	case "inspect_call_graph_metrics":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/call-graph/metrics", Body: map[string]any{
			"metric_type": args.String("metric_type"),
			"repo_id":     args.String("repo_id"),
			"language":    args.String("language"),
			"limit":       args.IntOr("limit", 25),
			"offset":      args.IntOr("offset", 0),
		}}, true
	case "trace_route_callers":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/routes/callers", Body: map[string]any{
			"repo_id":      args.String("repo_id"),
			"service_id":   args.String("service_id"),
			"service_name": args.String("service_name"),
			"method":       args.String("method"),
			"path":         args.String("path"),
			"max_depth":    args.IntOr("max_depth", 2),
			"limit":        args.IntOr("limit", 25),
		}}, true
	case "investigate_code_topic":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/topics/investigate", Body: map[string]any{
			"topic":    args.String("topic"),
			"intent":   args.String("intent"),
			"repo_id":  args.String("repo_id"),
			"language": args.String("language"),
			"limit":    args.IntOr("limit", 25),
			"offset":   args.IntOr("offset", 0),
		}}, true
	case "execute_language_query":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/language-query", Body: map[string]any{
			"language":    args.String("language"),
			"entity_type": args.String("entity_type"),
			"query":       args.String("query"),
			"repo_id":     args.String("repo_id"),
			"limit":       args.IntOr("limit", 50),
		}}, true
	case "find_function_call_chain":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/call-chain", Body: map[string]any{
			"start":           args.String("start"),
			"end":             args.String("end"),
			"repo_id":         args.String("repo_id"),
			"cross_repo":      args.BoolOr("cross_repo", false),
			"start_repo_id":   args.String("start_repo_id"),
			"end_repo_id":     args.String("end_repo_id"),
			"start_entity_id": args.String("start_entity_id"),
			"end_entity_id":   args.String("end_entity_id"),
			"max_depth":       args.IntOr("max_depth", 5),
		}}, true
	default:
		return routecontract.Request{}, false
	}
}
