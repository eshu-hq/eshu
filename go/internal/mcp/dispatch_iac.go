// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func iacManagementStatusBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_id":      str(args, "scope_id"),
		"account_id":    str(args, "account_id"),
		"region":        str(args, "region"),
		"arn":           str(args, "arn"),
		"resource_id":   str(args, "resource_id"),
		"finding_kinds": stringSlice(args, "finding_kinds"),
		"limit":         1,
		"offset":        0,
	}
}

func terraformImportPlanBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_id":      str(args, "scope_id"),
		"account_id":    str(args, "account_id"),
		"region":        str(args, "region"),
		"arn":           str(args, "arn"),
		"resource_id":   str(args, "resource_id"),
		"finding_kinds": stringSlice(args, "finding_kinds"),
		"limit":         intOr(args, "limit", 100),
		"offset":        intOr(args, "offset", 0),
	}
}

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

func terraformConfigStateDriftFindingsBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_id":    str(args, "scope_id"),
		"address":     str(args, "address"),
		"outcome":     str(args, "outcome"),
		"drift_kinds": stringSlice(args, "drift_kinds"),
		"limit":       intOr(args, "limit", 100),
		"offset":      intOr(args, "offset", 0),
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

func replatformingOwnershipBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_id":      str(args, "scope_id"),
		"account_id":    str(args, "account_id"),
		"region":        str(args, "region"),
		"finding_kinds": stringSlice(args, "finding_kinds"),
		"limit":         intOr(args, "limit", 100),
		"offset":        intOr(args, "offset", 0),
	}
}
