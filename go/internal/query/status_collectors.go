// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
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
		"source_systems":         row.SourceSystems,
		"observation_count":      row.ObservationCount,
		"last_observed_at":       nullableRFC3339(row.LastObservedAt),
		"updated_at":             nullableRFC3339(row.UpdatedAt),
		"deactivated_at":         nullableRFC3339(row.DeactivatedAt),
		"detail":                 row.Detail,
	}
}

func scopedCollectorRuntimeStatusesToSlice(rows []status.CollectorRuntimeStatus) []map[string]any {
	type aggregate struct {
		collectorKind              string
		runtimeMode                string
		statusCategory             string
		health                     string
		collectorCount             int
		coordinatorRegisteredCount int
		enabledCount               int
		bootstrapCount             int
		claimsEnabledCount         int
		observationCount           int
		lastObservedAt             time.Time
		updatedAt                  time.Time
		evidenceSources            map[string]struct{}
	}

	aggregates := map[string]*aggregate{}
	for _, row := range rows {
		key := strings.Join([]string{row.CollectorKind, row.RuntimeMode, row.StatusCategory, row.Health}, "\x00")
		current := aggregates[key]
		if current == nil {
			current = &aggregate{
				collectorKind:   row.CollectorKind,
				runtimeMode:     row.RuntimeMode,
				statusCategory:  row.StatusCategory,
				health:          row.Health,
				evidenceSources: map[string]struct{}{},
			}
			aggregates[key] = current
		}
		current.collectorCount++
		if row.CoordinatorRegistered {
			current.coordinatorRegisteredCount++
		}
		if row.Enabled {
			current.enabledCount++
		}
		if row.Bootstrap {
			current.bootstrapCount++
		}
		if row.ClaimsEnabled {
			current.claimsEnabledCount++
		}
		current.observationCount += row.ObservationCount
		if row.LastObservedAt.After(current.lastObservedAt) {
			current.lastObservedAt = row.LastObservedAt
		}
		if row.UpdatedAt.After(current.updatedAt) {
			current.updatedAt = row.UpdatedAt
		}
		for _, source := range row.EvidenceSources {
			if source = strings.TrimSpace(source); source != "" {
				current.evidenceSources[source] = struct{}{}
			}
		}
	}

	keys := make([]string, 0, len(aggregates))
	for key := range aggregates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	collectors := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		row := aggregates[key]
		collectors = append(collectors, map[string]any{
			"collector_kind":               row.collectorKind,
			"runtime_mode":                 row.runtimeMode,
			"status_category":              row.statusCategory,
			"health":                       row.health,
			"collector_count":              row.collectorCount,
			"coordinator_registered_count": row.coordinatorRegisteredCount,
			"enabled_count":                row.enabledCount,
			"bootstrap_count":              row.bootstrapCount,
			"claims_enabled_count":         row.claimsEnabledCount,
			"evidence_sources":             sortedCollectorEvidenceSources(row.evidenceSources),
			"observation_count":            row.observationCount,
			"last_observed_at":             nullableRFC3339(row.lastObservedAt),
			"updated_at":                   nullableRFC3339(row.updatedAt),
		})
	}
	return collectors
}

func sortedCollectorEvidenceSources(sources map[string]struct{}) []string {
	result := make([]string, 0, len(sources))
	for source := range sources {
		result = append(result, source)
	}
	sort.Strings(result)
	return result
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
