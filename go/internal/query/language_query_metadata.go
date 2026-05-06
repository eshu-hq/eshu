package query

import (
	"context"
	"fmt"
)

func (h *LanguageQueryHandler) enrichLanguageResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	language string,
	label string,
	query string,
	repoID string,
	limit int,
) ([]map[string]any, error) {
	if h == nil || h.Content == nil || len(results) == 0 {
		return results, nil
	}

	entityType := graphLabelToContentEntityType(label)
	if entityType == "" {
		return results, nil
	}

	for i := range results {
		attachSemanticSummary(results[i])
	}

	rows, err := h.Content.SearchEntitiesByLanguageAndType(
		ctx,
		repoID,
		language,
		entityType,
		query,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("enrich language results with content metadata: %w", err)
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
		key := languageResultMatchKey(
			StringVal(results[i], "file_path"),
			label,
			StringVal(results[i], "name"),
			IntVal(results[i], "start_line"),
		)
		metadata, ok := metadataByKey[key]
		if !ok || len(metadata) == 0 {
			continue
		}
		results[i]["metadata"] = mergeGraphFirstMetadata(results[i]["metadata"], metadata)
		attachSemanticSummary(results[i])
	}

	return results, nil
}

func languageResultMatchKey(filePath string, entityType string, name string, startLine int) string {
	return fmt.Sprintf("%s|%s|%s|%d", filePath, entityType, name, startLine)
}

func mergeGraphFirstMetadata(existing any, fallback map[string]any) map[string]any {
	if len(fallback) == 0 {
		if metadata, ok := existing.(map[string]any); ok {
			return metadata
		}
		return nil
	}
	merged := make(map[string]any, len(fallback))
	for key, value := range fallback {
		merged[key] = value
	}
	if current, ok := existing.(map[string]any); ok {
		for key, value := range current {
			if value == nil {
				continue
			}
			merged[key] = value
		}
	}
	return merged
}
