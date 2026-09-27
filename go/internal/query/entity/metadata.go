// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type entityMetadataCandidate struct {
	index int
	id    string
	key   querycontract.EntityContentKey
}

// EnrichEntityResultsWithContentMetadata fills sparse graph-row metadata from
// repository-scoped content rows. The name and limit parameters remain for the
// root compatibility wrapper; the bounded graph result set determines the
// exact IDs and location keys read from the content store.
func (h *Handler) EnrichEntityResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	repoID string,
	_ string,
	_ int,
) ([]map[string]any, error) {
	if h == nil || h.Content == nil || len(results) == 0 || repoID == "" {
		for i := range results {
			if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
				attachSemanticSummary(results[i])
			}
		}
		return results, nil
	}

	candidates := make([]entityMetadataCandidate, 0, len(results))
	ids := make([]string, 0, len(results))
	for i := range results {
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			attachSemanticSummary(results[i])
			continue
		}
		entityType := taxonomy.ResultContentEntityType(results[i])
		if entityType == "" {
			continue
		}
		id := querycontract.StringVal(results[i], "id")
		candidates = append(candidates, entityMetadataCandidate{
			index: i,
			id:    id,
			key: querycontract.EntityContentKey{
				RelativePath: querycontract.StringVal(results[i], "file_path"),
				EntityType:   entityType,
				EntityName:   querycontract.StringVal(results[i], "name"),
				StartLine:    querycontract.IntVal(results[i], "start_line"),
			},
		})
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(candidates) == 0 {
		return results, nil
	}

	byID := make(map[string]querycontract.EntityContent, len(ids))
	if len(ids) > 0 {
		rows, err := h.Content.ListRepoEntitiesByIDs(ctx, repoID, ids, len(ids))
		if err != nil {
			return nil, fmt.Errorf("enrich entity results by ID: %w", err)
		}
		for _, row := range rows {
			if row.RepoID == repoID {
				byID[row.EntityID] = row
			}
		}
	}

	fallback := make([]entityMetadataCandidate, 0, len(candidates))
	keys := make([]querycontract.EntityContentKey, 0, len(candidates))
	idHits := 0
	for _, candidate := range candidates {
		row, ok := byID[candidate.id]
		if ok && entityContentKey(row) == candidate.key {
			idHits++
			attachEntityContentMetadata(results[candidate.index], row.Metadata)
			continue
		}
		fallback = append(fallback, candidate)
		keys = append(keys, candidate.key)
	}

	keyHits, missing := 0, 0
	if len(keys) > 0 {
		rows, err := h.Content.ListRepoEntitiesByKeys(ctx, repoID, keys)
		if err != nil {
			return nil, fmt.Errorf("enrich entity results by exact key: %w", err)
		}
		byKey := make(map[querycontract.EntityContentKey][]querycontract.EntityContent, len(rows))
		for _, row := range rows {
			if row.RepoID != repoID {
				continue
			}
			key := entityContentKey(row)
			byKey[key] = append(byKey[key], row)
		}
		for _, candidate := range fallback {
			matches := byKey[candidate.key]
			if len(matches) > 1 {
				recordEntityMetadataHydration(ctx, idHits, keyHits, missing, 1)
				return nil, fmt.Errorf("enrich entity results: ambiguous content entity location for graph result")
			}
			if len(matches) == 0 {
				missing++
				continue
			}
			keyHits++
			attachEntityContentMetadata(results[candidate.index], matches[0].Metadata)
		}
	}
	recordEntityMetadataHydration(ctx, idHits, keyHits, missing, 0)
	return results, nil
}

func entityContentKey(row querycontract.EntityContent) querycontract.EntityContentKey {
	return querycontract.EntityContentKey{
		RelativePath: row.RelativePath,
		EntityType:   row.EntityType,
		EntityName:   row.EntityName,
		StartLine:    row.StartLine,
	}
}

func attachEntityContentMetadata(result map[string]any, metadata map[string]any) {
	if len(metadata) == 0 {
		return
	}
	result["metadata"] = metadata
	attachSemanticSummary(result)
}

func recordEntityMetadataHydration(ctx context.Context, idHits, keyHits, missing, ambiguous int) {
	trace.SpanFromContext(ctx).AddEvent("query.entity_metadata_hydration", trace.WithAttributes(
		attribute.Int("entity.metadata.id_hits", idHits),
		attribute.Int("entity.metadata.key_hits", keyHits),
		attribute.Int("entity.metadata.missing", missing),
		attribute.Int("entity.metadata.ambiguous", ambiguous),
	))
}
