// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/status"

func terraformStateStatusToMap(report status.TerraformStateReport) map[string]any {
	summary := make([]map[string]any, 0, len(report.WarningSummary))
	for _, row := range report.WarningSummary {
		summary = append(summary, map[string]any{
			"warning_kind":  row.WarningKind,
			"reason":        row.Reason,
			"scope_class":   row.ScopeClass,
			"severity":      row.Severity,
			"actionability": row.Actionability,
			"count":         row.Count,
		})
	}
	recentWarnings := make([]map[string]any, 0, len(report.RecentWarnings))
	for _, row := range report.RecentWarnings {
		recentWarnings = append(recentWarnings, map[string]any{
			"safe_locator_hash": row.SafeLocatorHash,
			"backend_kind":      row.BackendKind,
			"warning_kind":      row.WarningKind,
			"reason":            row.Reason,
			"severity":          row.Severity,
			"actionability":     row.Actionability,
			"source":            row.Source,
			"source_handle":     row.SourceHandle,
		})
	}
	return map[string]any{
		"warning_summary": summary,
		"recent_warnings": recentWarnings,
	}
}

// withTerraformStateSource adds the terraform_state_source object (#7009) that
// says whether the terraform_state section came from the stored summary or the
// live statements and how old it is. It has the shape and the closed value sets
// of active_work_source. It adds no key when the status read skipped Terraform
// evidence (the ingester, operations, hosted readiness, and the other routes
// that never render the section) or when the reader reports no source. It
// returns payload for chaining.
func withTerraformStateSource(payload map[string]any, source status.ActiveWorkSource) map[string]any {
	if view := source.JSON(); view != nil {
		payload["terraform_state_source"] = view
	}
	return payload
}
