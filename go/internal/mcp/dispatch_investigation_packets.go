// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func investigationPacketRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "export_supply_chain_impact_packet":
		return &route{
			method: "GET",
			path:   "/api/v0/investigations/supply-chain/impact/packet",
			query: investigationPacketQuery(args,
				"finding_id", "advisory_id", "cve_id", "package_id", "repository_id",
				"subject_digest", "image_ref", "workload_id", "service_id"),
		}, true
	case "export_deployable_unit_packet":
		return &route{
			method: "GET",
			path:   "/api/v0/investigations/deployable-unit/packet",
			query:  investigationPacketQuery(args, "scope_id", "generation_id", "repository_id", "repo_id"),
		}, true
	case "export_cloud_runtime_drift_packet":
		return &route{
			method: "GET",
			path:   "/api/v0/investigations/drift/packet",
			query: investigationPacketQuery(args,
				"scope_id", "account_id", "project_id", "subscription_id",
				"provider", "cloud_resource_uid"),
		}, true
	default:
		return nil, false
	}
}

func investigationPacketQuery(args map[string]any, stringKeys ...string) map[string]string {
	q := make(map[string]string, len(stringKeys)+1)
	for _, key := range stringKeys {
		q[key] = str(args, key)
	}
	if raw, ok := args["max_source_facts"]; ok && raw != nil {
		q["max_source_facts"] = strconv.Itoa(intOr(args, "max_source_facts", 0))
	}
	return q
}
