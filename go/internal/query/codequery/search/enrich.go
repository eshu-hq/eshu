// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// EnrichResultsWithContentMetadata fills in missing per-result metadata
// by graph entity ID in one repository-scoped content-store read. Results
// that already carry metadata are left untouched.
func EnrichResultsWithContentMetadata(
	ctx context.Context,
	store querycontract.ContentStore,
	results []map[string]any,
	repoID string,
	_ string,
	_ int,
) ([]map[string]any, error) {
	if len(results) == 0 {
		return results, nil
	}

	ids := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for i := range results {
		metadata, ok := results[i]["metadata"].(map[string]any)
		if ok && len(metadata) > 0 {
			entitysemantics.AttachSemanticSummary(results[i])
			continue
		}
		id := querycontract.StringVal(results[i], "entity_id")
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) == 0 || store == nil {
		return results, nil
	}

	rows, err := store.ListRepoEntitiesByIDs(ctx, repoID, ids, len(ids))
	if err != nil {
		return nil, fmt.Errorf("enrich graph search results by entity id: %w", err)
	}
	if len(rows) == 0 {
		return results, nil
	}

	metadataByID := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		metadataByID[row.EntityID] = row.Metadata
	}

	for i := range results {
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			continue
		}
		metadata, ok := metadataByID[querycontract.StringVal(results[i], "entity_id")]
		if !ok || len(metadata) == 0 {
			continue
		}
		results[i]["metadata"] = metadata
		entitysemantics.AttachSemanticSummary(results[i])
	}

	return results, nil
}

// EnrichResultsWithContentMetadataByEntityID fills in missing
// per-result metadata with one content-store read per entity id,
// merging over any metadata the result already carries.
func EnrichResultsWithContentMetadataByEntityID(
	ctx context.Context,
	store querycontract.ContentStore,
	results []map[string]any,
) ([]map[string]any, error) {
	if store == nil || len(results) == 0 {
		for i := range results {
			if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
				entitysemantics.AttachSemanticSummary(results[i])
			}
		}
		return results, nil
	}

	for i := range results {
		entityID := querycontract.StringVal(results[i], "entity_id")
		if entityID == "" {
			continue
		}
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			entitysemantics.AttachSemanticSummary(results[i])
		}
		entity, err := store.GetEntityContent(ctx, entityID)
		if err != nil {
			return nil, fmt.Errorf("enrich graph results by entity id: %w", err)
		}
		if entity == nil || len(entity.Metadata) == 0 {
			continue
		}
		results[i]["metadata"] = MergeMetadata(results[i]["metadata"], entity.Metadata)
		entitysemantics.AttachSemanticSummary(results[i])
	}

	return results, nil
}

// MergeMetadata overlays content metadata onto the graph result's own
// metadata without overwriting keys the graph already set. Either side
// may be absent.
func MergeMetadata(existing any, content map[string]any) map[string]any {
	if len(content) == 0 {
		merged, _ := existing.(map[string]any)
		if len(merged) == 0 {
			return nil
		}
		return cloneMap(merged)
	}

	merged, _ := existing.(map[string]any)
	if len(merged) == 0 {
		return cloneMap(content)
	}

	result := cloneMap(merged)
	for key, value := range content {
		if _, ok := result[key]; ok {
			continue
		}
		result[key] = value
	}
	return result
}

// cloneMap copies a row map, preserving nil. It duplicates
// cloneQueryAnyMap's nil contract rather than reusing
// querycontract.CloneAnyMap, which normalizes empty to an empty map --
// MergeMetadata's absent-metadata shape is nil, and the wire must keep
// telling the two apart.
func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
