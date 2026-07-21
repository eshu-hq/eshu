// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

func deploymentTraceCloudResourcesFromRows(rows []map[string]any, defaultRelationshipBasis string) ([]map[string]any, error) {
	groupedRows := groupCloudResourceObservationRows(rows)
	resources := make([]map[string]any, 0, len(groupedRows))
	for _, row := range groupedRows {
		row, err := cloudResourceRowWithSelectedObservation(row)
		if err != nil {
			return nil, err
		}
		resources = append(resources, map[string]any{
			"id":                    StringVal(row, "id"),
			"name":                  StringVal(row, "name"),
			"kind":                  StringVal(row, "kind"),
			"resource_type":         StringVal(row, "resource_type"),
			"provider":              StringVal(row, "provider"),
			"environment":           StringVal(row, "environment"),
			"confidence":            floatVal(row, "confidence"),
			"reason":                StringVal(row, "reason"),
			"relationship_basis":    firstNonEmptyString(StringVal(row, "relationship_basis"), defaultRelationshipBasis),
			"resolution_mode":       StringVal(row, "resolution_mode"),
			"evidence_source":       StringVal(row, "evidence_source"),
			"service_anchor_source": StringVal(row, "service_anchor_source"),
			"service_anchor_reason": StringVal(row, "service_anchor_reason"),
			"source_fact_id":        StringVal(row, "source_fact_id"),
			"stable_fact_key":       StringVal(row, "stable_fact_key"),
			"source_system":         StringVal(row, "source_system"),
			"source_record_id":      StringVal(row, "source_record_id"),
			"collector_kind":        StringVal(row, "collector_kind"),
		})
	}
	return resources, nil
}

func groupCloudResourceObservationRows(rows []map[string]any) []map[string]any {
	grouped := make([]map[string]any, 0, len(rows))
	indexByID := make(map[string]int, len(rows))
	for _, row := range rows {
		if len(mapSliceValue(row, "observations")) > 0 {
			grouped = append(grouped, row)
			continue
		}
		resourceID := StringVal(row, "id")
		observation := cloudResourceObservationFromRow(row)
		if index, ok := indexByID[resourceID]; ok {
			observations := mapSliceValue(grouped[index], "observations")
			grouped[index]["observations"] = append(observations, observation)
			continue
		}
		resource := map[string]any{
			"id":            resourceID,
			"name":          StringVal(row, "name"),
			"kind":          StringVal(row, "kind"),
			"resource_type": StringVal(row, "resource_type"),
			"provider":      StringVal(row, "provider"),
			"observations":  []map[string]any{observation},
		}
		indexByID[resourceID] = len(grouped)
		grouped = append(grouped, resource)
	}
	return grouped
}

func cloudResourceObservationFromRow(row map[string]any) map[string]any {
	if observation := mapValue(row, "observation"); len(observation) > 0 {
		copied := make(map[string]any, len(observation)+1)
		for key, value := range observation {
			copied[key] = value
		}
		copied["environment"] = firstNonEmptyString(
			StringVal(observation, "environment"),
			StringVal(row, "resource_environment"),
			StringVal(row, "instance_environment"),
		)
		return copied
	}
	return map[string]any{
		"environment":           StringVal(row, "environment"),
		"confidence":            row["confidence"],
		"reason":                StringVal(row, "reason"),
		"relationship_basis":    StringVal(row, "relationship_basis"),
		"resolution_mode":       StringVal(row, "resolution_mode"),
		"evidence_source":       StringVal(row, "evidence_source"),
		"service_anchor_source": StringVal(row, "service_anchor_source"),
		"service_anchor_reason": StringVal(row, "service_anchor_reason"),
		"source_fact_id":        StringVal(row, "source_fact_id"),
		"stable_fact_key":       StringVal(row, "stable_fact_key"),
		"source_system":         StringVal(row, "source_system"),
		"source_record_id":      StringVal(row, "source_record_id"),
		"collector_kind":        StringVal(row, "collector_kind"),
	}
}

func cloudResourceRowWithSelectedObservation(row map[string]any) (map[string]any, error) {
	observations := mapSliceValue(row, "observations")
	if len(observations) == 0 {
		return row, nil
	}
	for _, observation := range observations {
		if _, err := finiteGraphFloat(
			observation,
			"confidence",
			fmt.Sprintf("cloud resource observation for %q", StringVal(row, "id")),
		); err != nil {
			return nil, err
		}
	}
	selected := append([]map[string]any(nil), observations...)
	sort.SliceStable(selected, func(left, right int) bool {
		leftConfidence := floatVal(selected[left], "confidence")
		rightConfidence := floatVal(selected[right], "confidence")
		if leftConfidence != rightConfidence {
			return leftConfidence > rightConfidence
		}
		return cloudResourceObservationKey(selected[left]) < cloudResourceObservationKey(selected[right])
	})
	merged := make(map[string]any, len(row)+len(selected[0]))
	for key, value := range row {
		if key != "observations" {
			merged[key] = value
		}
	}
	for key, value := range selected[0] {
		merged[key] = value
	}
	return merged, nil
}

func cloudResourceObservationKey(observation map[string]any) string {
	return strings.Join([]string{
		StringVal(observation, "stable_fact_key"),
		StringVal(observation, "source_fact_id"),
		StringVal(observation, "source_system"),
		StringVal(observation, "source_record_id"),
		StringVal(observation, "relationship_basis"),
		StringVal(observation, "resolution_mode"),
		StringVal(observation, "evidence_source"),
		StringVal(observation, "service_anchor_source"),
		StringVal(observation, "service_anchor_reason"),
		StringVal(observation, "collector_kind"),
		StringVal(observation, "environment"),
		StringVal(observation, "reason"),
	}, "\x00")
}

func deploymentTraceCloudCandidates(rows []map[string]any) []map[string]any {
	candidates := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		candidate := make(map[string]any, len(row)+3)
		for key, value := range row {
			candidate[key] = value
		}
		candidate["candidate_status"] = "uncorrelated"
		candidate["match_basis"] = firstNonEmptyString(
			StringVal(row, "relationship_basis"),
			"deployment_config_read_evidence",
		)
		candidate["missing_relationship"] = "workload_cloud_relationship"
		candidates = append(candidates, candidate)
	}
	return candidates
}
