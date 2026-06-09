package mcp

import (
	"fmt"
	"net/url"
	"strings"
)

func statusRoute(toolName string, args map[string]any) (*route, bool, error) {
	switch toolName {
	case "list_collectors":
		return &route{method: "GET", path: "/api/v0/status/collectors"}, true, nil
	case "list_ingesters":
		return &route{method: "GET", path: "/api/v0/status/ingesters"}, true, nil
	case "get_ingester_status":
		ingester := str(args, "ingester")
		if ingester == "" {
			ingester = "repository"
		}
		return &route{method: "GET", path: "/api/v0/status/ingesters/" + url.PathEscape(ingester)}, true, nil
	case "get_index_status":
		return &route{method: "GET", path: "/api/v0/index-status"}, true, nil
	case "get_hosted_readiness":
		return &route{method: "GET", path: "/api/v0/status/hosted-readiness"}, true, nil
	case "get_hosted_governance_status":
		return &route{method: "GET", path: "/api/v0/status/governance"}, true, nil
	case "get_semantic_capability_status":
		return &route{method: "GET", path: "/api/v0/status/semantic-extraction"}, true, nil
	case "list_component_extensions":
		return &route{method: "GET", path: "/api/v0/component-extensions", query: map[string]string{
			"limit": intString(args, "limit", 100),
		}}, true, nil
	case "get_component_extension_diagnostics":
		componentID := strings.TrimSpace(str(args, "component_id"))
		if componentID == "" {
			return nil, true, fmt.Errorf("component_id is required")
		}
		return &route{
			method: "GET",
			path:   "/api/v0/component-extensions/" + url.PathEscape(componentID) + "/diagnostics",
		}, true, nil
	default:
		return nil, false, nil
	}
}
