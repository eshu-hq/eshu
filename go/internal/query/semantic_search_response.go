// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// semanticSearchResponseFromRetrieval maps a bounded retrieval response into the
// wire response. When rerank is requested it applies graph-neighborhood
// reranking over the in-scope results, attaches a per-result ranking basis, and
// reports the reranking state and recommended next calls. Reranking is purely a
// reordering of the already-retrieved set, so the truth labels, scores, and
// scope of every result are unchanged.
//
// Facet counts are derived from the post-filter result set — the documents
// already returned by the bounded index query — so no second scan is issued.
func semanticSearchResponseFromRetrieval(
	req searchretrieval.Request,
	retrieval searchretrieval.Response,
	indexResult semanticSearchIndexResult,
	rerank bool,
) semanticSearchResponse {
	ordered, rerankInfo, nextCalls := semanticSearchRerankResults(req, retrieval, rerank)
	results := make([]semanticSearchResult, 0, len(ordered))
	langCounts := make(map[string]int)
	for _, item := range ordered {
		result := item.result
		doc := semanticSearchDocumentFromSearchDoc(result.Document)
		results = append(results, semanticSearchResult{
			Rank:         item.rank,
			Score:        result.Score,
			SearchMethod: semanticSearchMethod(result.Metadata, retrieval.Mode),
			Document:     doc,
			GraphHandles: semanticSearchGraphHandles(result.Handles),
			TruthScope:   semanticSearchTruthScope(result.TruthScope),
			Freshness:    semanticSearchFreshness(result.Freshness),
			Failures:     append([]searchbench.FailureClass(nil), result.Failures...),
			Metadata:     cloneSemanticSearchMetadata(result.Metadata),
			RankingBasis: item.basis,
		})
		for _, label := range doc.Labels {
			if lang, ok := strings.CutPrefix(label, "language:"); ok && lang != "" {
				langCounts[lang]++
			}
		}
	}
	return semanticSearchResponse{
		Query:                    retrieval.Query,
		RepoID:                   req.Scope.RepoID,
		Anchor:                   retrieval.Anchor,
		Mode:                     retrieval.Mode,
		SearchMode:               string(retrieval.Mode),
		Limit:                    retrieval.Limit,
		TimeoutMS:                int(retrieval.Timeout / time.Millisecond),
		Results:                  results,
		Truncated:                retrieval.Truncated,
		FalseCanonicalClaimCount: retrieval.FalseCanonicalClaimCount,
		IndexedDocumentCount:     indexResult.IndexedDocumentCount,
		CorpusLimit:              indexResult.CorpusLimit,
		CorpusMayBeTruncated:     indexResult.CorpusMayBeTruncated,
		RetrievalState:           indexResult.RetrievalState,
		Facets:                   semanticSearchFacets{Languages: langCounts},
		Rerank:                   rerankInfo,
		RecommendedNextCalls:     nextCalls,
	}
}

func semanticSearchDocumentFromSearchDoc(doc searchdocs.Document) semanticSearchDocument {
	return semanticSearchDocument{
		ID:           doc.ID,
		RepoID:       doc.RepoID,
		SourceKind:   doc.SourceKind,
		Title:        doc.Title,
		Path:         doc.Path,
		ContextText:  doc.ContextText,
		EntityRefs:   semanticSearchEntityRefs(doc.EntityRefs),
		GraphHandles: semanticSearchGraphHandles(doc.GraphHandles),
		Labels:       append([]string(nil), doc.Labels...),
		UpdatedAt:    doc.UpdatedAt,
		TruthScope:   semanticSearchTruthScope(doc.TruthScope),
		Freshness:    semanticSearchFreshness(doc.Freshness),
		AccessScope:  semanticSearchAccessScope{RepoID: doc.AccessScope.RepoID},
		Provenance: semanticSearchProvenance{
			SourceTable: doc.Provenance.SourceTable,
			SourceIDs:   append([]string(nil), doc.Provenance.SourceIDs...),
		},
	}
}

func semanticSearchEntityRefs(refs []searchdocs.EntityRef) []semanticSearchEntityRef {
	out := make([]semanticSearchEntityRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, semanticSearchEntityRef{
			ID:        ref.ID,
			Type:      ref.Type,
			Name:      ref.Name,
			Path:      ref.Path,
			StartLine: ref.StartLine,
			EndLine:   ref.EndLine,
		})
	}
	return out
}

func semanticSearchGraphHandles(handles []searchdocs.GraphHandle) []semanticSearchGraphHandle {
	out := make([]semanticSearchGraphHandle, 0, len(handles))
	for _, handle := range handles {
		out = append(out, semanticSearchGraphHandle{Kind: handle.Kind, ID: handle.ID})
	}
	return out
}

func semanticSearchMethod(metadata map[string]string, mode searchbench.Mode) string {
	if method := strings.TrimSpace(metadata["search_method"]); method != "" {
		return method
	}
	return string(mode)
}

func cloneSemanticSearchMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
