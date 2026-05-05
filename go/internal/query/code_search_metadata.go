package query

import (
	"context"
	"fmt"
)

func (h *CodeHandler) enrichGraphSearchResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	repoID string,
	query string,
	limit int,
) ([]map[string]any, error) {
	if len(results) == 0 {
		return results, nil
	}

	allHaveMetadata := true
	for i := range results {
		metadata, ok := results[i]["metadata"].(map[string]any)
		if !ok || len(metadata) == 0 {
			allHaveMetadata = false
			continue
		}
		attachSemanticSummary(results[i])
	}

	if allHaveMetadata || h == nil || h.Content == nil {
		return results, nil
	}

	rows, err := h.Content.SearchEntityContent(ctx, repoID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("enrich graph search results with content metadata: %w", err)
	}
	if len(rows) == 0 {
		return results, nil
	}

	metadataByKey := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		metadataByKey[languageResultMatchKey(
			row.RelativePath,
			row.EntityType,
			row.EntityName,
			row.StartLine,
		)] = row.Metadata
	}

	for i := range results {
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			continue
		}
		entityType := resultContentEntityType(results[i])
		if entityType == "" {
			continue
		}
		key := languageResultMatchKey(
			StringVal(results[i], "file_path"),
			entityType,
			StringVal(results[i], "name"),
			IntVal(results[i], "start_line"),
		)
		metadata, ok := metadataByKey[key]
		if !ok || len(metadata) == 0 {
			continue
		}
		results[i]["metadata"] = metadata
		attachSemanticSummary(results[i])
	}

	return results, nil
}

func (h *CodeHandler) enrichGraphResultsWithContentMetadataByEntityID(
	ctx context.Context,
	results []map[string]any,
) ([]map[string]any, error) {
	if h == nil || h.Content == nil || len(results) == 0 {
		for i := range results {
			if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
				attachSemanticSummary(results[i])
			}
		}
		return results, nil
	}

	for i := range results {
		entityID := StringVal(results[i], "entity_id")
		if entityID == "" {
			continue
		}
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			attachSemanticSummary(results[i])
		}
		entity, err := h.Content.GetEntityContent(ctx, entityID)
		if err != nil {
			return nil, fmt.Errorf("enrich graph results by entity id: %w", err)
		}
		if entity == nil || len(entity.Metadata) == 0 {
			continue
		}
		results[i]["metadata"] = mergeGraphAndContentMetadata(results[i]["metadata"], entity.Metadata)
		attachSemanticSummary(results[i])
	}

	return results, nil
}

func resultContentEntityType(result map[string]any) string {
	labels := StringSliceVal(result, "labels")
	for _, label := range labels {
		if entityType := graphLabelToContentEntityType(label); entityType != "" {
			return entityType
		}
	}
	return ""
}

func mergeGraphAndContentMetadata(existing any, content map[string]any) map[string]any {
	if len(content) == 0 {
		merged, _ := existing.(map[string]any)
		if len(merged) == 0 {
			return nil
		}
		return cloneQueryAnyMap(merged)
	}

	merged, _ := existing.(map[string]any)
	if len(merged) == 0 {
		return cloneQueryAnyMap(content)
	}

	result := cloneQueryAnyMap(merged)
	for key, value := range content {
		if _, ok := result[key]; ok {
			continue
		}
		result[key] = value
	}
	return result
}

func cloneQueryAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
