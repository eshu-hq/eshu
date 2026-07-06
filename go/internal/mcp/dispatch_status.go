// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	case "get_operator_control_plane":
		return &route{method: "GET", path: "/api/v0/status/operator-control-plane"}, true, nil
	case "list_dead_letter_work_items":
		limit := intOr(args, "limit", 0)
		if limit <= 0 {
			return nil, true, fmt.Errorf("limit is required")
		}
		timeoutMS := intOr(args, "timeout_ms", 0)
		if timeoutMS <= 0 {
			return nil, true, fmt.Errorf("timeout_ms is required")
		}
		body := map[string]any{
			"limit":      limit,
			"timeout_ms": timeoutMS,
		}
		for _, key := range []string{
			"failure_class",
			"domain",
			"scope_id",
			"collector_kind",
			"updated_after",
			"updated_before",
		} {
			if value := strings.TrimSpace(str(args, key)); value != "" {
				body[key] = value
			}
		}
		return &route{method: "POST", path: "/api/v0/admin/dead-letters/query", body: body}, true, nil
	case "get_freshness_causality":
		return &route{method: "GET", path: "/api/v0/status/freshness-causality"}, true, nil
	case "get_collector_readiness":
		return &route{method: "GET", path: "/api/v0/status/collector-readiness"}, true, nil
	case "get_hosted_governance_status":
		return &route{method: "GET", path: "/api/v0/status/governance"}, true, nil
	case "get_semantic_capability_status":
		return &route{method: "GET", path: "/api/v0/status/semantic-extraction"}, true, nil
	case "get_answer_narration_status":
		return &route{method: "GET", path: "/api/v0/status/answer-narration"}, true, nil
	case "get_capability_catalog":
		query := map[string]string{
			"limit":  intString(args, "limit", 200),
			"offset": intString(args, "offset", 0),
		}
		if maturity := str(args, "maturity"); maturity != "" {
			query["maturity"] = maturity
		}
		if owner := str(args, "owner"); owner != "" {
			query["owner"] = owner
		}
		return &route{method: "GET", path: "/api/v0/capabilities", query: query}, true, nil
	case "get_surface_inventory":
		query := map[string]string{
			"limit":  intString(args, "limit", 200),
			"offset": intString(args, "offset", 0),
		}
		if category := str(args, "category"); category != "" {
			query["category"] = category
		}
		if readiness := str(args, "readiness"); readiness != "" {
			query["readiness"] = readiness
		}
		return &route{method: "GET", path: "/api/v0/surface-inventory", query: query}, true, nil
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
	case "list_collector_extraction_readiness":
		return &route{method: "GET", path: "/api/v0/collector-extraction-readiness", query: map[string]string{
			"limit": intString(args, "limit", 100),
		}}, true, nil
	case "get_collector_extraction_readiness":
		family := strings.TrimSpace(str(args, "family"))
		if family == "" {
			return nil, true, fmt.Errorf("family is required")
		}
		return &route{
			method: "GET",
			path:   "/api/v0/collector-extraction-readiness/" + url.PathEscape(family),
		}, true, nil
	case "list_fact_schema_versions":
		return &route{method: "GET", path: "/api/v0/fact-schema-versions", query: map[string]string{
			"limit": intString(args, "limit", 200),
		}}, true, nil
	case "get_fact_schema_version":
		factKind := strings.TrimSpace(str(args, "fact_kind"))
		if factKind == "" {
			return nil, true, fmt.Errorf("fact_kind is required")
		}
		factRoute := &route{
			method: "GET",
			path:   "/api/v0/fact-schema-versions/" + url.PathEscape(factKind),
		}
		if candidate := strings.TrimSpace(str(args, "candidate")); candidate != "" {
			factRoute.query = map[string]string{"candidate": candidate}
		}
		return factRoute, true, nil
	default:
		return nil, false, nil
	}
}
