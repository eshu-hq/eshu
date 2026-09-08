// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// EnrichEntityResultsWithContentMetadata fills sparse result-row metadata
// from the content store. Exported for the staying root metadata tests that
// pin enrichment against a real content reader; see #6060.
func (h *EntityHandler) EnrichEntityResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	repoID string,
	query string,
	limit int,
) ([]map[string]any, error) {
	if h == nil || h.Content == nil || len(results) == 0 {
		for i := range results {
			if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
				attachSemanticSummary(results[i])
			}
		}
		return results, nil
	}

	allHaveMetadata := true
	for i := range results {
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			attachSemanticSummary(results[i])
			continue
		}
		allHaveMetadata = false
	}

	if allHaveMetadata {
		return results, nil
	}

	rows, err := h.Content.SearchEntityContent(ctx, repoID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("enrich entity results with content metadata: %w", err)
	}
	if len(rows) == 0 {
		return results, nil
	}

	metadataByKey := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		metadataByKey[querycontract.LanguageResultMatchKey(
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
		entityType := querycontract.ResultContentEntityType(results[i])
		if entityType == "" {
			continue
		}
		key := querycontract.LanguageResultMatchKey(
			querycontract.StringVal(results[i], "file_path"),
			entityType,
			querycontract.StringVal(results[i], "name"),
			querycontract.IntVal(results[i], "start_line"),
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
