// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// cloudRuntimeDriftRoute maps the provider-neutral runtime drift readback tool to
// its internal POST route. It forwards only the bounded, non-sensitive filter
// parameters; the handler validates provider and finding_kinds against closed
// sets, requires a canonical scope, and gates the capability by runtime profile.
func cloudRuntimeDriftRoute(toolName string, args map[string]any) (*route, bool) {
	if toolName != "list_cloud_runtime_drift_findings" {
		return nil, false
	}
	return &route{
		method: "POST",
		path:   "/api/v0/cloud/runtime-drift/findings",
		body:   cloudRuntimeDriftBody(args),
	}, true
}

func cloudRuntimeDriftBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_id":           str(args, "scope_id"),
		"account_id":         str(args, "account_id"),
		"project_id":         str(args, "project_id"),
		"subscription_id":    str(args, "subscription_id"),
		"provider":           str(args, "provider"),
		"cloud_resource_uid": str(args, "cloud_resource_uid"),
		"finding_kinds":      stringSlice(args, "finding_kinds"),
		"limit":              intOr(args, "limit", 100),
		"offset":             intOr(args, "offset", 0),
	}
}
