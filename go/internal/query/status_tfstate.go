package query

import "github.com/eshu-hq/eshu/go/internal/status"

func terraformStateStatusToMap(report status.TerraformStateReport) map[string]any {
	summary := make([]map[string]any, 0, len(report.WarningSummary))
	for _, row := range report.WarningSummary {
		summary = append(summary, map[string]any{
			"warning_kind": row.WarningKind,
			"reason":       row.Reason,
			"scope_class":  row.ScopeClass,
			"count":        row.Count,
		})
	}
	return map[string]any{
		"warning_summary": summary,
	}
}
