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
