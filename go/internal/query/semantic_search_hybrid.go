// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

const semanticSearchLocalHybridCorpusLimit = 500

// SemanticSearchHybridStore searches active curated documents with a configured
// local hybrid backend.
type SemanticSearchHybridStore interface {
	Search(context.Context, semanticSearchIndexQuery) (semanticSearchIndexResult, error)
}

// SemanticSearchDocumentStore loads active curated search documents for the
// request-time local hybrid adapter.
type SemanticSearchDocumentStore interface {
	ListActiveDocuments(context.Context, semanticSearchDocumentQuery) ([]semanticSearchDocumentRow, error)
}

type semanticSearchDocumentQuery struct {
	ScopeID     string
	RepoID      string
	SourceKinds []searchdocs.SourceKind
	Limit       int
}

type semanticSearchDocumentRow struct {
	Document searchdocs.Document
}

// LocalSemanticSearchHybrid builds a deterministic no-network hybrid index for
// one bounded request.
type LocalSemanticSearchHybrid struct {
	Documents SemanticSearchDocumentStore
}

// NewLocalSemanticSearchHybrid creates a request-time local hybrid retrieval
// adapter over active curated search documents.
func NewLocalSemanticSearchHybrid(documents SemanticSearchDocumentStore) *LocalSemanticSearchHybrid {
	return &LocalSemanticSearchHybrid{Documents: documents}
}

// Search loads active documents for one repository scope and runs the pure-Go
// hybrid backend with the deterministic hash embedder.
func (h *LocalSemanticSearchHybrid) Search(
	ctx context.Context,
	query semanticSearchIndexQuery,
) (semanticSearchIndexResult, error) {
	if h == nil || h.Documents == nil {
		return semanticSearchIndexResult{}, fmt.Errorf("local semantic search hybrid document store is required")
	}
	rows, err := h.Documents.ListActiveDocuments(ctx, semanticSearchDocumentQuery{
		ScopeID:     query.ScopeID,
		RepoID:      query.RepoID,
		SourceKinds: query.SourceKinds,
		Limit:       semanticSearchLocalHybridCorpusLimit,
	})
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	docs := make([]searchdocs.Document, 0, len(rows))
	for _, row := range rows {
		docs = append(docs, row.Document)
	}
	embedder, err := searchembed.NewHashEmbedder(searchembed.DefaultDimensions)
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{
		MaxDocuments: semanticSearchLocalHybridCorpusLimit,
		Embedder:     embedder,
	})
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	candidates, err := (searchhybrid.Backend{Index: index}).Search(ctx, query.Request)
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	return semanticSearchIndexResult{
		Candidates:           candidates,
		IndexedDocumentCount: index.Size(),
		CorpusLimit:          semanticSearchLocalHybridCorpusLimit,
		CorpusMayBeTruncated: index.Overflow() > 0 || len(rows) >= semanticSearchLocalHybridCorpusLimit,
		RetrievalState:       semanticSearchActiveRetrievalState(query.Request.Mode, candidates),
	}, nil
}

func defaultSemanticSearchRetrievalState(mode searchbench.Mode) string {
	switch mode {
	case searchbench.ModeSemantic:
		return "semantic_unavailable"
	case searchbench.ModeHybrid:
		return "hybrid_degraded"
	default:
		return "keyword_only"
	}
}

func semanticSearchActiveRetrievalState(
	mode searchbench.Mode,
	candidates []searchretrieval.Candidate,
) string {
	switch mode {
	case searchbench.ModeSemantic:
		return "semantic_active"
	case searchbench.ModeHybrid:
		for _, candidate := range candidates {
			if candidate.Metadata["search_method"] == "rrf_hybrid" {
				return "hybrid_active"
			}
		}
		return "hybrid_degraded"
	default:
		return "keyword_only"
	}
}
