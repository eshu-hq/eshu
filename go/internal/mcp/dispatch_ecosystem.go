// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// ecosystemRoute maps ecosystem-summary tools to their bounded internal HTTP
// endpoints. It is split out of resolveRoute's main switch so dispatch.go stays
// under the file-size cap and ecosystem-summary routing stays cohesive.
func ecosystemRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "get_ecosystem_overview":
		return &route{method: "GET", path: "/api/v0/ecosystem/overview"}, true
	case "get_graph_summary_packet":
		return &route{method: "POST", path: "/api/v0/ecosystem/graph-summary", body: map[string]any{
			"repo_id": str(args, "repo_id"),
			"limit":   intOr(args, "limit", 10),
		}}, true
	case "analyze_pre_change_impact":
		return &route{method: "POST", path: "/api/v0/impact/pre-change", body: map[string]any{
			"target":        str(args, "target"),
			"target_type":   str(args, "target_type"),
			"service_name":  str(args, "service_name"),
			"workload_id":   str(args, "workload_id"),
			"resource_id":   str(args, "resource_id"),
			"module_id":     str(args, "module_id"),
			"topic":         str(args, "topic"),
			"repo_id":       str(args, "repo_id"),
			"base_ref":      str(args, "base_ref"),
			"head_ref":      str(args, "head_ref"),
			"changed_paths": stringSlice(args, "changed_paths"),
			"changes":       objectSlice(args, "changes"),
			"environment":   str(args, "environment"),
			"max_depth":     intOr(args, "max_depth", 4),
			"limit":         intOr(args, "limit", 25),
			"offset":        intOr(args, "offset", 0),
		}}, true
	case "plan_developer_change":
		return &route{method: "POST", path: "/api/v0/impact/developer-change-plan", body: map[string]any{
			"developer_intent": str(args, "developer_intent"),
			"target":           str(args, "target"),
			"target_type":      str(args, "target_type"),
			"service_name":     str(args, "service_name"),
			"workload_id":      str(args, "workload_id"),
			"resource_id":      str(args, "resource_id"),
			"module_id":        str(args, "module_id"),
			"topic":            str(args, "topic"),
			"repo_id":          str(args, "repo_id"),
			"base_ref":         str(args, "base_ref"),
			"head_ref":         str(args, "head_ref"),
			"changed_paths":    stringSlice(args, "changed_paths"),
			"changes":          objectSlice(args, "changes"),
			"environment":      str(args, "environment"),
			"max_depth":        intOr(args, "max_depth", 4),
			"limit":            intOr(args, "limit", 25),
			"offset":           intOr(args, "offset", 0),
		}}, true
	default:
		return nil, false
	}
}

// compareRoute maps environment-comparison tools to their bounded internal HTTP
// endpoints. Split out of resolveRoute's main switch to keep dispatch.go under
// the file-size cap.
func compareRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "compare_environments":
		return &route{method: "POST", path: "/api/v0/compare/environments", body: map[string]any{
			"workload_id": str(args, "workload_id"),
			"left":        str(args, "left"),
			"right":       str(args, "right"),
			"limit":       intOr(args, "limit", 50),
		}}, true
	default:
		return nil, false
	}
}
