// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
)

// enrichLanguageResultsWithContentMetadata merges Postgres content-index
// metadata into graph-sourced results, keyed by file path/label/name/start
// line. merged reports true whenever a matched row's content metadata was
// non-empty and was merged into that row via mergeGraphFirstMetadata -- every
// no-op path below (nil Content, unmapped label, zero content rows, no key
// match, or a matched key whose content metadata is empty) reports
// merged=false. This is not the same as "at least one content VALUE actually
// changed the row": mergeGraphFirstMetadata lets any non-nil graph-derived
// value in the row's existing metadata override the content value at the
// same key, so a row whose content metadata keys are all shadowed by
// non-nil graph values still reports merged=true even though the final
// metadata map is unchanged from the graph-only answer. The direction stays
// safe either way -- this can only over-claim toward a hybrid/derived truth
// basis when a plain graph read would have been equally accurate, never
// launder a content-served answer as authoritative-graph-only (#5761 P1-1).
func (h *LanguageQueryHandler) enrichLanguageResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	language string,
	label string,
	query string,
	repoID string,
	limit int,
) ([]map[string]any, bool, error) {
	if h == nil || h.Content == nil || len(results) == 0 {
		return results, false, nil
	}

	entityType := graphLabelToContentEntityType(label)
	if entityType == "" {
		return results, false, nil
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
		return nil, false, fmt.Errorf("enrich language results with content metadata: %w", err)
	}
	if len(rows) == 0 {
		return results, false, nil
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

	merged := false
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
		merged = true
	}

	return results, merged, nil
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
