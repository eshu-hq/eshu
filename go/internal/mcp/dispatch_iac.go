// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// iacManagementStatusBody, terraformImportPlanBody,
// terraformConfigStateDriftFindingsBody, and replatformingOwnershipBody
// moved to internal/mcp/iacmanagement (managementStatusBody and the inline
// bodies in Route) with the tools that used them. The three body builders
// below stay here: compose_replatforming_plan, list_aws_runtime_drift_findings,
// and get_replatforming_rollups are not part of the IaC-management family
// (see the iacManagementRoute doc comment in dispatch.go).

func replatformingPlanBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_kind":    str(args, "scope_kind"),
		"scope_id":      str(args, "scope_id"),
		"account_id":    str(args, "account_id"),
		"region":        str(args, "region"),
		"service_name":  str(args, "service_name"),
		"workload_id":   str(args, "workload_id"),
		"repo_id":       str(args, "repo_id"),
		"environment":   str(args, "environment"),
		"arn":           str(args, "arn"),
		"resource_id":   str(args, "resource_id"),
		"finding_kinds": stringSlice(args, "finding_kinds"),
		"limit":         intOr(args, "limit", 100),
		"offset":        intOr(args, "offset", 0),
	}
}

func awsRuntimeDriftFindingsBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_id":      str(args, "scope_id"),
		"account_id":    str(args, "account_id"),
		"region":        str(args, "region"),
		"arn":           str(args, "arn"),
		"finding_kinds": stringSlice(args, "finding_kinds"),
		"limit":         intOr(args, "limit", 100),
		"offset":        intOr(args, "offset", 0),
	}
}

func replatformingRollupsBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_id":      str(args, "scope_id"),
		"account_id":    str(args, "account_id"),
		"region":        str(args, "region"),
		"finding_kinds": stringSlice(args, "finding_kinds"),
		"limit":         intOr(args, "limit", 100),
		"offset":        intOr(args, "offset", 0),
	}
}
