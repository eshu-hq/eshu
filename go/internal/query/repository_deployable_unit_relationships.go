// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

func queryRepoDeployableUnitRelationshipOverview(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
) []map[string]any {
	outgoing := queryRepoRelationshipOverviewDirection(ctx, reader, params, `
		MATCH (r:Repository {id: $repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(target:Repository)
		RETURN 'outgoing' AS direction,
		       type(rel) AS type,
		       r.name AS source_name,
		       r.id AS source_id,
		       target.name AS target_name,
		       target.id AS target_id,
		       rel.evidence_type AS evidence_type,
		       rel.resolved_id AS resolved_id,
		       rel.generation_id AS generation_id,
		       rel.confidence AS confidence,
		       rel.evidence_count AS evidence_count,
		       rel.evidence_kinds AS evidence_kinds,
		       rel.resolution_source AS resolution_source,
		       rel.rationale AS rationale
		ORDER BY target_name
	`)
	incoming := queryRepoRelationshipOverviewDirection(ctx, reader, params, `
		MATCH (source:Repository)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(r:Repository {id: $repo_id})
		RETURN 'incoming' AS direction,
		       type(rel) AS type,
		       source.name AS source_name,
		       source.id AS source_id,
		       r.name AS target_name,
		       r.id AS target_id,
		       rel.evidence_type AS evidence_type,
		       rel.resolved_id AS resolved_id,
		       rel.generation_id AS generation_id,
		       rel.confidence AS confidence,
		       rel.evidence_count AS evidence_count,
		       rel.evidence_kinds AS evidence_kinds,
		       rel.resolution_source AS resolution_source,
		       rel.rationale AS rationale
		ORDER BY source_name
	`)
	if len(outgoing) == 0 {
		return incoming
	}
	return append(outgoing, incoming...)
}

func mergeRepositoryDeployableUnitRelationships(
	readModel *repositoryRelationshipReadModel,
	supplemental []map[string]any,
) *repositoryRelationshipReadModel {
	if readModel == nil || len(supplemental) == 0 {
		return readModel
	}
	relationships := mergeRepositoryRelationshipRows(readModel.Relationships, supplemental)
	return &repositoryRelationshipReadModel{
		Available:     readModel.Available,
		Relationships: relationships,
		Consumers:     repositoryConsumersFromRelationships(relationships),
	}
}

func mergeRepositoryRelationshipRows(
	base []map[string]any,
	supplemental []map[string]any,
) []map[string]any {
	rows := make([]map[string]any, 0, len(base)+len(supplemental))
	seen := make(map[string]struct{}, len(base)+len(supplemental))
	for _, row := range append(base, supplemental...) {
		key := repositoryRelationshipRowKey(row)
		if key == "" {
			rows = append(rows, row)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		rows = append(rows, row)
	}
	return rows
}

func repositoryRelationshipRowKey(row map[string]any) string {
	parts := []string{
		StringVal(row, "direction"),
		StringVal(row, "type"),
		StringVal(row, "source_id"),
		StringVal(row, "target_id"),
		StringVal(row, "resolved_id"),
		StringVal(row, "generation_id"),
	}
	if strings.Join(parts, "") == "" {
		return ""
	}
	return strings.Join(parts, "\x00")
}

func repositoryConsumersFromRelationships(relationships []map[string]any) []map[string]any {
	consumers := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for _, row := range relationships {
		if StringVal(row, "direction") != "incoming" {
			continue
		}
		sourceID := StringVal(row, "source_id")
		sourceName := StringVal(row, "source_name")
		if sourceID == "" && sourceName == "" {
			continue
		}
		key := sourceID + "\x00" + sourceName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		consumers = append(consumers, map[string]any{
			"id":   sourceID,
			"name": sourceName,
		})
	}
	return consumers
}
