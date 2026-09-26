// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// rerankFileResults applies the bounded hybrid re-rank to file-search rows when
// a ranker is configured and the request resolved to a single repository scope.
// The re-rank is fallback-safe: when no ranker is set, the scope is not a single
// repo, or the ranker reports applied=false, the lexical content-index order is
// returned unchanged with no search_backend marker.
func (h *ContentHandler) rerankFileResults(ctx context.Context, req contentSearchRequest, results []querycontract.FileContent) []FileContent {
	if h.HybridRanker == nil || len(results) < 2 {
		return results
	}
	repoID := req.repoID()
	if repoID == "" {
		return results
	}
	reranked, _ := h.HybridRanker.RerankFiles(ctx, repoID, req.pattern(), results)
	return reranked
}

// rerankEntityResults applies the bounded hybrid re-rank to entity-search rows;
// see rerankFileResults for the single-repo scope guard and fallback contract.
func (h *ContentHandler) rerankEntityResults(ctx context.Context, req contentSearchRequest, results []querycontract.EntityContent) []querycontract.EntityContent {
	if h.HybridRanker == nil || len(results) < 2 {
		return results
	}
	repoID := req.repoID()
	if repoID == "" {
		return results
	}
	reranked, _ := h.HybridRanker.RerankEntities(ctx, repoID, req.pattern(), results)
	return reranked
}

// entityContentSearchResponse shapes the entity search page into its response
// body. The read-time source_cache clip runs here, last: the rerank above reads
// the full stored body, and the clip count covers only the page returned
// (#7171).
func entityContentSearchResponse(results []querycontract.EntityContent, req contentSearchRequest, truncated bool) map[string]any {
	rows := querycontract.EntityContentSearchRows(results)
	clippedRows := querycontract.ClipRowsSourceCache(rows)
	response := contentSearchResponse(rows, req, truncated)
	querycontract.AddSourceCacheClipMarkers(response, clippedRows)
	return response
}
