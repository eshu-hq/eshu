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
