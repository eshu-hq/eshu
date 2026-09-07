// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func DeploymentTraceCloudResourcesFromRows(rows []map[string]any, defaultRelationshipBasis string) ([]map[string]any, error) {
	groupedRows := groupCloudResourceObservationRows(rows)
	resources := make([]map[string]any, 0, len(groupedRows))
	for _, row := range groupedRows {
		row, err := cloudResourceRowWithSelectedObservation(row)
		if err != nil {
			return nil, err
		}
		resources = append(resources, map[string]any{
			"id":                    querycontract.StringVal(row, "id"),
			"name":                  querycontract.StringVal(row, "name"),
			"kind":                  querycontract.StringVal(row, "kind"),
			"resource_type":         querycontract.StringVal(row, "resource_type"),
			"provider":              querycontract.StringVal(row, "provider"),
			"environment":           querycontract.StringVal(row, "environment"),
			"confidence":            querycontract.FloatVal(row, "confidence"),
			"reason":                querycontract.StringVal(row, "reason"),
			"relationship_basis":    querycontract.FirstNonEmptyString(querycontract.StringVal(row, "relationship_basis"), defaultRelationshipBasis),
			"resolution_mode":       querycontract.StringVal(row, "resolution_mode"),
			"evidence_source":       querycontract.StringVal(row, "evidence_source"),
			"service_anchor_source": querycontract.StringVal(row, "service_anchor_source"),
			"service_anchor_reason": querycontract.StringVal(row, "service_anchor_reason"),
			"source_fact_id":        querycontract.StringVal(row, "source_fact_id"),
			"stable_fact_key":       querycontract.StringVal(row, "stable_fact_key"),
			"source_system":         querycontract.StringVal(row, "source_system"),
			"source_record_id":      querycontract.StringVal(row, "source_record_id"),
			"collector_kind":        querycontract.StringVal(row, "collector_kind"),
		})
	}
	return resources, nil
}

func groupCloudResourceObservationRows(rows []map[string]any) []map[string]any {
	grouped := make([]map[string]any, 0, len(rows))
	indexByID := make(map[string]int, len(rows))
	for _, row := range rows {
		if len(querycontract.MapSliceValue(row, "observations")) > 0 {
			grouped = append(grouped, row)
			continue
		}
		resourceID := querycontract.StringVal(row, "id")
		observation := cloudResourceObservationFromRow(row)
		if index, ok := indexByID[resourceID]; ok {
			observations := querycontract.MapSliceValue(grouped[index], "observations")
			grouped[index]["observations"] = append(observations, observation)
			continue
		}
		resource := map[string]any{
			"id":            resourceID,
			"name":          querycontract.StringVal(row, "name"),
			"kind":          querycontract.StringVal(row, "kind"),
			"resource_type": querycontract.StringVal(row, "resource_type"),
			"provider":      querycontract.StringVal(row, "provider"),
			"observations":  []map[string]any{observation},
		}
		indexByID[resourceID] = len(grouped)
		grouped = append(grouped, resource)
	}
	return grouped
}

func cloudResourceObservationFromRow(row map[string]any) map[string]any {
	if observation := querycontract.MapValue(row, "observation"); len(observation) > 0 {
		copied := make(map[string]any, len(observation)+1)
		for key, value := range observation {
			copied[key] = value
		}
		copied["environment"] = querycontract.FirstNonEmptyString(
			querycontract.StringVal(observation, "environment"),
			querycontract.StringVal(row, "resource_environment"),
			querycontract.StringVal(row, "instance_environment"),
		)
		return copied
	}
	return map[string]any{
		"environment":           querycontract.StringVal(row, "environment"),
		"confidence":            row["confidence"],
		"reason":                querycontract.StringVal(row, "reason"),
		"relationship_basis":    querycontract.StringVal(row, "relationship_basis"),
		"resolution_mode":       querycontract.StringVal(row, "resolution_mode"),
		"evidence_source":       querycontract.StringVal(row, "evidence_source"),
		"service_anchor_source": querycontract.StringVal(row, "service_anchor_source"),
		"service_anchor_reason": querycontract.StringVal(row, "service_anchor_reason"),
		"source_fact_id":        querycontract.StringVal(row, "source_fact_id"),
		"stable_fact_key":       querycontract.StringVal(row, "stable_fact_key"),
		"source_system":         querycontract.StringVal(row, "source_system"),
		"source_record_id":      querycontract.StringVal(row, "source_record_id"),
		"collector_kind":        querycontract.StringVal(row, "collector_kind"),
	}
}

func cloudResourceRowWithSelectedObservation(row map[string]any) (map[string]any, error) {
	observations := querycontract.MapSliceValue(row, "observations")
	if len(observations) == 0 {
		return row, nil
	}
	for _, observation := range observations {
		if _, err := querycontract.FiniteGraphFloat(
			observation,
			"confidence",
			fmt.Sprintf("cloud resource observation for %q", querycontract.StringVal(row, "id")),
		); err != nil {
			return nil, err
		}
	}
	selected := append([]map[string]any(nil), observations...)
	sort.SliceStable(selected, func(left, right int) bool {
		leftConfidence := querycontract.FloatVal(selected[left], "confidence")
		rightConfidence := querycontract.FloatVal(selected[right], "confidence")
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
		querycontract.StringVal(observation, "stable_fact_key"),
		querycontract.StringVal(observation, "source_fact_id"),
		querycontract.StringVal(observation, "source_system"),
		querycontract.StringVal(observation, "source_record_id"),
		querycontract.StringVal(observation, "relationship_basis"),
		querycontract.StringVal(observation, "resolution_mode"),
		querycontract.StringVal(observation, "evidence_source"),
		querycontract.StringVal(observation, "service_anchor_source"),
		querycontract.StringVal(observation, "service_anchor_reason"),
		querycontract.StringVal(observation, "collector_kind"),
		querycontract.StringVal(observation, "environment"),
		querycontract.StringVal(observation, "reason"),
	}, "\x00")
}

func DeploymentTraceCloudCandidates(rows []map[string]any) []map[string]any {
	candidates := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		candidate := make(map[string]any, len(row)+3)
		for key, value := range row {
			candidate[key] = value
		}
		candidate["candidate_status"] = "uncorrelated"
		candidate["match_basis"] = querycontract.FirstNonEmptyString(
			querycontract.StringVal(row, "relationship_basis"),
			"deployment_config_read_evidence",
		)
		candidate["missing_relationship"] = "workload_cloud_relationship"
		candidates = append(candidates, candidate)
	}
	return candidates
}
