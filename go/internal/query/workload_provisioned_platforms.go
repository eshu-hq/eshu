// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type provisionedPlatformResult struct {
	rows   []map[string]any
	limits map[string]any
}

func (h *EntityHandler) fetchProvisionedPlatformResult(ctx context.Context, repoID string) (provisionedPlatformResult, error) {
	if h == nil || h.Neo4j == nil || strings.TrimSpace(repoID) == "" {
		return provisionedPlatformResult{rows: []map[string]any{}}, nil
	}
	queryLimit := contextStoryItemLimit + 1
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() || !access.allowsRepositoryID(repoID) {
		return provisionedPlatformResult{rows: []map[string]any{}}, nil
	}
	scopeClause := ""
	if access.scoped() {
		scopeClause = "WHERE " + access.graphCondition("target") + " AND " + access.graphCondition("repo")
	}
	params := access.graphParams(map[string]any{"repo_id": repoID, "provisioned_platform_limit": queryLimit})
	rows, err := h.Neo4j.Run(ctx, fmt.Sprintf(`
		MATCH (target:Repository {id: $repo_id})<-[dependency:PROVISIONS_DEPENDENCY_FOR]-(repo:Repository)-[platformEdge:PROVISIONS_PLATFORM]->(p:Platform)
		%s
		WITH repo.id as platform_source_id, repo.name as platform_source_name,
		     target.id as platform_dependency_target_id,
		     p.id as platform_id, p.name as platform_name, p.kind as platform_kind,
		     p.provider as platform_provider, p.region as platform_region, p.locator as platform_locator,
		     collect(DISTINCT properties(dependency)) as dependency_edges,
		     collect(DISTINCT properties(platformEdge)) as platform_edges
		RETURN platform_source_id, platform_source_name, platform_dependency_target_id,
		       platform_id, platform_name, platform_kind, platform_provider, platform_region, platform_locator,
		       dependency_edges, platform_edges
		ORDER BY platform_name, platform_id, platform_source_id, platform_dependency_target_id
		LIMIT $provisioned_platform_limit
	`, scopeClause), params)
	if err != nil {
		return provisionedPlatformResult{}, err
	}
	normalized := make([]map[string]any, 0, min(len(rows), contextStoryItemLimit))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		platform := normalizeProvisionedPlatform(row)
		key := StringVal(platform, "platform_id") + "\x00" + StringVal(row, "platform_source_id") + "\x00" + StringVal(row, "platform_dependency_target_id")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if len(normalized) < contextStoryItemLimit {
			normalized = append(normalized, platform)
		}
	}
	sort.Slice(normalized, func(i, j int) bool {
		return StringVal(normalized[i], "platform_name")+"\x00"+StringVal(normalized[i], "platform_id") <
			StringVal(normalized[j], "platform_name")+"\x00"+StringVal(normalized[j], "platform_id")
	})
	truncated := len(rows) >= queryLimit || len(seen) > contextStoryItemLimit
	return provisionedPlatformResult{
		rows: normalized,
		limits: boundedCollectionMetadata(
			contextStoryItemLimit, queryLimit, len(normalized), len(rows), truncated,
			[]string{"platform_name", "platform_id", "source_repository_id", "target_repository_id"},
		),
	}, nil
}

func normalizeProvisionedPlatform(row map[string]any) map[string]any {
	row = copyStringAnyMap(row)
	if len(mapValue(row, "dependency_edge")) == 0 {
		row["dependency_edge"] = deterministicEvidenceProperties(row, "dependency_edges")
	}
	if len(mapValue(row, "platform_edge")) == 0 {
		row["platform_edge"] = deterministicEvidenceProperties(row, "platform_edges")
	}
	dependencyEdge := mapValue(row, "dependency_edge")
	platformEdge := mapValue(row, "platform_edge")
	row["dependency_confidence"] = floatVal(dependencyEdge, "confidence")
	row["dependency_reason"] = StringVal(dependencyEdge, "reason")
	row["platform_edge_confidence"] = floatVal(platformEdge, "confidence")
	row["platform_edge_reason"] = StringVal(platformEdge, "reason")
	return map[string]any{
		"platform_id":         StringVal(row, "platform_id"),
		"platform_name":       StringVal(row, "platform_name"),
		"platform_kind":       StringVal(row, "platform_kind"),
		"platform_provider":   StringVal(row, "platform_provider"),
		"platform_region":     StringVal(row, "platform_region"),
		"platform_locator":    StringVal(row, "platform_locator"),
		"platform_confidence": firstPositiveFloat(floatVal(row, "platform_edge_confidence"), floatVal(row, "dependency_confidence")),
		"platform_reason":     firstNonEmptyString(StringVal(row, "platform_edge_reason"), StringVal(row, "dependency_reason")),
		"topology_basis":      "provisioning_fallback",
		"topology_edges":      provisionedPlatformTopologyEdges(row),
	}
}

func deterministicEvidenceProperties(row map[string]any, field string) map[string]any {
	candidates := mapSliceValue(row, field)
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return stablePropertiesKey(candidates[i]) < stablePropertiesKey(candidates[j])
	})
	return copyStringAnyMap(candidates[0])
}
