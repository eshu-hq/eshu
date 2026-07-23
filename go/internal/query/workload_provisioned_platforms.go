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

type orderedProvisionedPlatform struct {
	platform map[string]any
	sourceID string
	targetID string
}

func (h *EntityHandler) fetchProvisionedPlatformResult(ctx context.Context, repoID string) (provisionedPlatformResult, error) {
	if h == nil || h.Neo4j == nil || strings.TrimSpace(repoID) == "" {
		return emptyProvisionedPlatformResult(), nil
	}
	queryLimit := contextStoryItemLimit + 1
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() || !access.allowsRepositoryID(repoID) {
		return emptyProvisionedPlatformResult(), nil
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
	// Collect every distinct (platform_id, source_id, target_id) tuple from
	// the bounded row set FIRST, with no length cap. Only after the full
	// distinct set is sorted into a deterministic order do we truncate to
	// contextStoryItemLimit. Capping `ordered` mid-walk (the #5644 bug) let
	// the 50 survivors depend on backend row order instead of stable
	// provisionedPlatformOrderKey identity.
	ordered := make([]orderedProvisionedPlatform, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		platform, err := normalizeProvisionedPlatform(row)
		if err != nil {
			return provisionedPlatformResult{}, err
		}
		sourceID := StringVal(row, "platform_source_id")
		targetID := StringVal(row, "platform_dependency_target_id")
		key := StringVal(platform, "platform_id") + "\x00" + sourceID + "\x00" + targetID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		ordered = append(ordered, orderedProvisionedPlatform{
			platform: platform,
			sourceID: sourceID,
			targetID: targetID,
		})
	}
	sort.Slice(ordered, func(i, j int) bool {
		return provisionedPlatformOrderKey(ordered[i]) < provisionedPlatformOrderKey(ordered[j])
	})
	distinctCount := len(ordered)
	truncatedByCap := distinctCount > contextStoryItemLimit
	if truncatedByCap {
		ordered = ordered[:contextStoryItemLimit]
	}
	normalized := make([]map[string]any, 0, len(ordered))
	for _, entry := range ordered {
		normalized = append(normalized, entry.platform)
	}
	truncated := len(rows) >= queryLimit || truncatedByCap
	return provisionedPlatformResult{
		rows: normalized,
		limits: boundedCollectionMetadata(
			contextStoryItemLimit, queryLimit, len(normalized), len(rows), truncated,
			[]string{"platform_name", "platform_id", "source_repository_id", "target_repository_id"},
		),
	}, nil
}

func emptyProvisionedPlatformResult() provisionedPlatformResult {
	return provisionedPlatformResult{
		rows: []map[string]any{},
		limits: emptyBoundedCollectionMetadata(
			contextStoryItemLimit,
			[]string{"platform_name", "platform_id", "source_repository_id", "target_repository_id"},
		),
	}
}

func provisionedPlatformOrderKey(entry orderedProvisionedPlatform) string {
	return strings.Join([]string{
		StringVal(entry.platform, "platform_name"),
		StringVal(entry.platform, "platform_id"),
		entry.sourceID,
		entry.targetID,
	}, "\x00")
}

func normalizeProvisionedPlatform(row map[string]any) (map[string]any, error) {
	row = copyStringAnyMap(row)
	if len(mapValue(row, "dependency_edge")) == 0 {
		properties, err := deterministicEvidenceProperties(row, "dependency_edges")
		if err != nil {
			return nil, fmt.Errorf("select dependency edge evidence: %w", err)
		}
		row["dependency_edge"] = properties
	}
	if len(mapValue(row, "platform_edge")) == 0 {
		properties, err := deterministicEvidenceProperties(row, "platform_edges")
		if err != nil {
			return nil, fmt.Errorf("select platform edge evidence: %w", err)
		}
		row["platform_edge"] = properties
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
	}, nil
}

func deterministicEvidenceProperties(row map[string]any, field string) (map[string]any, error) {
	candidates := mapSliceValue(row, field)
	if len(candidates) == 0 {
		return nil, nil
	}
	best := candidates[0]
	bestKey, err := stablePropertiesKey(best)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates[1:] {
		key, err := stablePropertiesKey(candidate)
		if err != nil {
			return nil, err
		}
		if key < bestKey {
			best = candidate
			bestKey = key
		}
	}
	return copyStringAnyMap(best), nil
}
