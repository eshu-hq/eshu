package searchhybrid

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// Backend serves bounded keyword, semantic, and hybrid retrieval over a hybrid
// index, implementing searchretrieval.Backend.
type Backend struct {
	Index *Index
}

// Search ranks the in-scope curated documents for one bounded request. Keyword
// mode uses BM25, semantic mode uses vector cosine similarity, and hybrid mode
// fuses both with Reciprocal Rank Fusion (degenerating to BM25 when no embedder
// is configured). It returns up to limit+1 candidates so the retrieval runner
// can detect and report truncation.
func (backend Backend) Search(
	ctx context.Context,
	req searchretrieval.Request,
) ([]searchretrieval.Candidate, error) {
	if err := searchretrieval.ValidateRequest(req); err != nil {
		return nil, err
	}
	if backend.Index == nil {
		return nil, errors.New("searchhybrid backend requires an index")
	}
	index := backend.Index

	anchor := req.Scope.Anchor()
	inScope := func(i int) bool { return matchesAnchor(index.documents[i].doc, anchor) }

	// BM25 ranking is served from the inverted index: only postings of the query
	// terms are visited, so cost scales with matches, not corpus size.
	queryTerms := tokenCounts(req.Query)
	bm25Scores := index.bm25ScoredInScope(queryTerms, inScope)
	bm25Ranked := positiveScored(index.rankByScore(bm25Scores), bm25Scores)

	var vectorScores map[int]float64
	var vectorRanked []int
	useVector := false
	if req.Mode == searchbench.ModeSemantic || req.Mode == searchbench.ModeHybrid {
		if index.embedder != nil {
			queryVector, err := index.embedder.Embed(ctx, req.Query)
			if err != nil {
				return nil, fmt.Errorf("embed query: %w", err)
			}
			vectorScores = index.vectorScoresInScope(queryVector, inScope)
			vectorRanked = index.rankByScore(vectorScores)
			useVector = true
		} else if req.Mode == searchbench.ModeSemantic {
			return nil, errors.New("searchhybrid semantic mode requires an embedder")
		}
	}

	ordered, finalScores, method := backend.rankForMode(req, index, modeInputs{
		bm25Ranked:   bm25Ranked,
		bm25Scores:   bm25Scores,
		vectorRanked: vectorRanked,
		vectorScores: vectorScores,
		useVector:    useVector,
	})

	bm25Positions := rankPositions(bm25Ranked)
	vectorPositions := rankPositions(vectorRanked)
	limit := req.Limit + 1
	candidates := make([]searchretrieval.Candidate, 0, limit)
	for _, idx := range truncate(ordered, limit) {
		candidates = append(candidates, searchretrieval.Candidate{
			Document: index.documents[idx].doc,
			Score:    finalScores[idx],
			Failures: overflowFailures(index),
			Metadata: map[string]string{
				"search_method":  method,
				"bm25_rank":      strconv.Itoa(bm25Positions[idx]),
				"vector_rank":    strconv.Itoa(vectorPositions[idx]),
				"index_overflow": strconv.FormatBool(index.overflow > 0),
			},
		})
	}
	return candidates, nil
}

type modeInputs struct {
	bm25Ranked   []int
	bm25Scores   map[int]float64
	vectorRanked []int
	vectorScores map[int]float64
	useVector    bool
}

// rankForMode produces the final ordering, the score used for each document,
// and the reported search method for the request mode.
func (backend Backend) rankForMode(
	req searchretrieval.Request,
	index *Index,
	in modeInputs,
) ([]int, map[int]float64, string) {
	switch req.Mode {
	case searchbench.ModeKeyword:
		return in.bm25Ranked, in.bm25Scores, "bm25"
	case searchbench.ModeSemantic:
		return in.vectorRanked, in.vectorScores, "vector"
	default: // hybrid
		pool := candidatePoolSize(req.Limit)
		lists := [][]int{truncate(in.bm25Ranked, pool)}
		method := "bm25"
		if in.useVector {
			lists = append(lists, truncate(in.vectorRanked, pool))
			method = "rrf_hybrid"
		}
		fused := rrfFuse(lists, index.rrfK)
		return index.rankByScore(fused), fused, method
	}
}

func overflowFailures(index *Index) []searchbench.FailureClass {
	if index.overflow > 0 {
		return []searchbench.FailureClass{searchbench.FailureClassTruncation}
	}
	return nil
}

// matchesAnchor reports whether a curated document is inside the request scope.
// It mirrors the bounded scope-first contract: every retrieval resolves the
// smallest available anchor before ranking.
func matchesAnchor(doc searchdocs.Document, anchor searchretrieval.Anchor) bool {
	switch anchor.Kind {
	case searchretrieval.ScopeKindService:
		return hasHandle(doc, "service", anchor.ID)
	case searchretrieval.ScopeKindWorkload:
		return hasHandle(doc, "workload", anchor.ID)
	case searchretrieval.ScopeKindRepo:
		return doc.RepoID == anchor.ID || hasHandle(doc, "repository", anchor.ID)
	case searchretrieval.ScopeKindEnvironment:
		return hasHandle(doc, "environment", anchor.ID)
	default:
		return false
	}
}

func hasHandle(doc searchdocs.Document, kind string, id string) bool {
	for _, handle := range doc.GraphHandles {
		if handle.Kind == kind && handle.ID == id {
			return true
		}
	}
	return false
}
