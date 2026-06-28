// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "fmt"

// impactRoute maps impact-analysis tool names to their internal HTTP routes.
// It is called as the fallback from dispatch.go's resolveRoute default case.
// It returns (route, true, nil) on a match, (nil, false, err) when toolName
// is not an impact tool.
func impactRoute(toolName string, args map[string]any) (*route, bool, error) {
	switch toolName {
	case "trace_deployment_chain":
		return &route{method: "POST", path: "/api/v0/impact/trace-deployment-chain", body: map[string]any{
			"service_name":                 str(args, "service_name"),
			"direct_only":                  boolOr(args, "direct_only", true),
			"max_depth":                    intOr(args, "max_depth", 8),
			"include_related_module_usage": boolOr(args, "include_related_module_usage", false),
		}}, true, nil
	case "investigate_deployment_config":
		return &route{method: "POST", path: "/api/v0/impact/deployment-config-influence", body: map[string]any{
			"service_name": str(args, "service_name"),
			"workload_id":  str(args, "workload_id"),
			"environment":  str(args, "environment"),
			"limit":        intOr(args, "limit", 25),
		}}, true, nil
	case "find_blast_radius":
		return &route{method: "POST", path: "/api/v0/impact/blast-radius", body: map[string]any{
			"target":      str(args, "target"),
			"target_type": str(args, "target_type"),
			"limit":       intOr(args, "limit", 50),
		}}, true, nil
	case "find_change_surface":
		return &route{method: "POST", path: "/api/v0/impact/change-surface", body: map[string]any{
			"target":      str(args, "target"),
			"environment": str(args, "environment"),
			"limit":       intOr(args, "limit", 50),
		}}, true, nil
	case "investigate_contract_impact":
		return &route{method: "POST", path: "/api/v0/impact/contracts", body: map[string]any{
			"family":           str(args, "family"),
			"provider_repo_id": str(args, "provider_repo_id"),
			"consumer_repo_id": str(args, "consumer_repo_id"),
			"repo_id":          str(args, "repo_id"),
			"route":            str(args, "route"),
			"topic":            str(args, "topic"),
			"service_name":     str(args, "service_name"),
			"method":           str(args, "method"),
			"limit":            intOr(args, "limit", 25),
		}}, true, nil
	case "investigate_change_surface":
		return &route{method: "POST", path: "/api/v0/impact/change-surface/investigate", body: map[string]any{
			"target":        str(args, "target"),
			"target_type":   str(args, "target_type"),
			"service_name":  str(args, "service_name"),
			"workload_id":   str(args, "workload_id"),
			"resource_id":   str(args, "resource_id"),
			"module_id":     str(args, "module_id"),
			"topic":         str(args, "topic"),
			"repo_id":       str(args, "repo_id"),
			"changed_paths": stringSlice(args, "changed_paths"),
			"environment":   str(args, "environment"),
			"max_depth":     intOr(args, "max_depth", 4),
			"limit":         intOr(args, "limit", 25),
			"offset":        intOr(args, "offset", 0),
		}}, true, nil
	case "trace_resource_to_code":
		return &route{method: "POST", path: "/api/v0/impact/trace-resource-to-code", body: map[string]any{
			"start":       str(args, "start"),
			"environment": str(args, "environment"),
			"max_depth":   intOr(args, "max_depth", 8),
			"limit":       intOr(args, "limit", 50),
		}}, true, nil
	case "explain_dependency_path":
		return &route{method: "POST", path: "/api/v0/impact/explain-dependency-path", body: args}, true, nil
	case "trace_exposure_path":
		return &route{method: "POST", path: "/api/v0/impact/trace-exposure-path", body: map[string]any{
			"source":           str(args, "source"),
			"source_entity_id": str(args, "source_entity_id"),
			"repo_id":          str(args, "repo_id"),
			"max_depth":        intOr(args, "max_depth", 5),
		}}, true, nil
	default:
		return nil, false, fmt.Errorf("not an impact tool: %s", toolName)
	}
}
