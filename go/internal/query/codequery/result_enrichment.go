// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/search"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the thin *CodeHandler enrichment surface of the
// search family. Enrichment and the metadata merge live in the search
// leaf; the methods stay here because Go requires methods to live in
// their type's package. The dead-code investigation next-call shapers
// that shared this file moved to the deadcode leaf, which owns them.

// enrichGraphSearchResultsWithContentMetadata fills in missing
// per-result metadata through the search leaf. Relationships, quality,
// and the handler name this spelling.
func (h *CodeHandler) enrichGraphSearchResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	repoID string,
	query string,
	limit int,
) ([]map[string]any, error) {
	return search.EnrichResultsWithContentMetadata(ctx, contentStoreOf(h), results, repoID, query, limit)
}

// enrichGraphResultsWithContentMetadataByEntityID fills in missing
// per-result metadata by entity id through the search leaf. Tests name
// this spelling.
func (h *CodeHandler) enrichGraphResultsWithContentMetadataByEntityID(
	ctx context.Context,
	results []map[string]any,
) ([]map[string]any, error) {
	return search.EnrichResultsWithContentMetadataByEntityID(ctx, contentStoreOf(h), results)
}

// contentStoreOf maps a possibly nil handler to its content store so the
// leaf's nil-store path preserves the pre-move nil-handler behavior.
func contentStoreOf(h *CodeHandler) querycontract.ContentStore {
	if h == nil {
		return nil
	}
	return h.Content
}

// ResultContentEntityType resolves a result row's content-entity type from
// its graph labels. The implementation moved to querycontract with lane B5
// of #6060; this wrapper keeps codequery callers unchanged.
func ResultContentEntityType(result map[string]any) string {
	return querycontract.ResultContentEntityType(result)
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
