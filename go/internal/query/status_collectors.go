package query

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func collectorRuntimeStatusesToSlice(rows []status.CollectorRuntimeStatus) []map[string]any {
	collectors := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		collectors = append(collectors, collectorRuntimeStatusToMap(row))
	}
	return collectors
}

func collectorRuntimeStatusToMap(row status.CollectorRuntimeStatus) map[string]any {
	return map[string]any{
		"instance_id":            row.InstanceID,
		"collector_kind":         row.CollectorKind,
		"mode":                   row.Mode,
		"runtime_mode":           row.RuntimeMode,
		"status_category":        row.StatusCategory,
		"coordinator_registered": row.CoordinatorRegistered,
		"enabled":                row.Enabled,
		"bootstrap":              row.Bootstrap,
		"claims_enabled":         row.ClaimsEnabled,
		"display_name":           row.DisplayName,
		"health":                 row.Health,
		"evidence_sources":       row.EvidenceSources,
		"observation_count":      row.ObservationCount,
		"last_observed_at":       nullableRFC3339(row.LastObservedAt),
		"updated_at":             nullableRFC3339(row.UpdatedAt),
		"deactivated_at":         nullableRFC3339(row.DeactivatedAt),
		"detail":                 row.Detail,
	}
}

func collectorRuntimeUpdatedAt(rows []status.CollectorRuntimeStatus) any {
	var latest time.Time
	for _, row := range rows {
		if row.UpdatedAt.After(latest) {
			latest = row.UpdatedAt
		}
	}
	return nullableRFC3339(latest)
}
