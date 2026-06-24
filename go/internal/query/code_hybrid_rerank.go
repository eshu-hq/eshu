// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// codeHybridRerankLimit caps the candidate set the in-process hybrid index ranks
// for one find_code request. The handler already bounds its lexical result set
// to the request limit (default 50, hard maximum below this cap), so the index
// is built over a small, request-local document set and never the whole corpus.
const codeHybridRerankLimit = 100

// codeHybridRerankTimeout bounds the in-process re-rank pass. It guards the
// embedder and cosine scoring against a pathological corpus; the work is
// CPU-only over at most codeHybridRerankLimit request-local documents.
const codeHybridRerankTimeout = 2 * time.Second

// CodeHybridRanker reorders lexical find_code results by hybrid relevance.
//
// It fuses BM25 lexical scoring with vector cosine similarity (Reciprocal Rank
// Fusion) over the entities a find_code request already retrieved. The ranker
// never widens the candidate set, issues no graph or Postgres reads, and
// produces no canonical truth: it only changes the order of results the lexical
// path already authorized and returned.
//
// Embedding is deliberately confined to a process-local deterministic hash
// embedder. This bounded in-request re-rank MUST NOT call a governed provider
// embedder, because that would POST request-local source snippets to an external
// endpoint and block on the provider HTTP timeout per result, ignoring client
// cancellation and bypassing the semantic policy / document-vector readiness
// path. The local embedder keeps all source text inside the process.
type CodeHybridRanker struct {
	// enabled gates the re-rank pass; it is true only when the runtime's
	// semantic search is configured.
	enabled bool
	// localEmbedder is the deterministic no-network embedder built once at
	// construction. It is the only embedder the re-rank ever uses, and it is not
	// injectable, so a governed provider embedder can never embed source text on
	// this path.
	localEmbedder searchhybrid.Embedder
}

// NewCodeHybridRanker builds a find_code hybrid re-ranker. It owns a
// deterministic local hash embedder; no provider embedder is accepted, so source
// text never egresses on this path. When enabled is false, or the local embedder
// cannot be built, Rerank is a no-op and the caller keeps the lexical order.
func NewCodeHybridRanker(enabled bool) *CodeHybridRanker {
	ranker := &CodeHybridRanker{enabled: enabled}
	if !enabled {
		return ranker
	}
	if embedder, err := searchembed.NewHashEmbedder(searchembed.DefaultDimensions); err == nil {
		ranker.localEmbedder = embedder
	}
	return ranker
}

// CodeResultReranker reorders code-search result rows by hybrid relevance.
//
// Rerank returns the reordered rows and reports whether the hybrid pass changed
// the ranking input. When it returns applied=false the caller MUST keep the
// lexical ordering and lexical truth basis: the ranker found no in-scope vector
// or lexical signal to fuse (empty input, no query-term overlap, no projectable
// documents, or an unavailable embedder).
type CodeResultReranker interface {
	Rerank(ctx context.Context, repoID, query string, results []map[string]any) ([]map[string]any, bool)
}

// Rerank reorders results by fused BM25+vector rank for the repository scope.
//
// The candidate documents are projected from the lexical result rows with the
// same curated search-document projection the persisted index uses, embedded
// with the shipped local embedder, and ranked in hybrid mode. Results that do
// not project into a scoped document keep their lexical position appended after
// the ranked set, so the response never drops a row the lexical path returned.
func (r *CodeHybridRanker) Rerank(
	ctx context.Context,
	repoID string,
	query string,
	results []map[string]any,
) ([]map[string]any, bool) {
	if r == nil || !r.enabled || r.localEmbedder == nil || repoID == "" || len(results) < 2 {
		return results, false
	}
	if err := ctx.Err(); err != nil {
		return results, false
	}

	docByID, order := projectCodeResultDocuments(repoID, results)
	if len(docByID) < 2 {
		return results, false
	}

	docs := make([]searchdocs.Document, 0, len(docByID))
	for _, id := range order {
		docs = append(docs, docByID[id])
	}

	rankCtx, cancel := context.WithTimeout(ctx, codeHybridRerankTimeout)
	defer cancel()

	// The local hash embedder is deterministic and CPU-only; NewIndex embeds the
	// projected documents here with no network call. The provider embedder is
	// never reachable from this path.
	index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{
		MaxDocuments:    codeHybridRerankLimit,
		Embedder:        r.localEmbedder,
		VectorRetrieval: searchhybrid.VectorRetrievalAuto,
	})
	if err != nil {
		return results, false
	}

	req := searchretrieval.Request{
		Query:   query,
		Scope:   searchretrieval.Scope{RepoID: repoID},
		Mode:    searchbench.ModeHybrid,
		Limit:   len(docs),
		Timeout: codeHybridRerankTimeout,
	}
	if err := searchretrieval.ValidateRequest(req); err != nil {
		return results, false
	}

	candidates, err := (searchhybrid.Backend{Index: index}).Search(rankCtx, req)
	if err != nil || len(candidates) == 0 {
		return results, false
	}

	return reorderCodeResultsByCandidates(results, candidates), true
}

// projectCodeResultDocuments builds curated search documents keyed by entity id
// from lexical result rows. It reuses searchdocs.ProjectContentEntity so the
// searchable text and graph handles match the persisted search-document index,
// keeping the in-process re-rank consistent with the durable retrieval lane.
func projectCodeResultDocuments(
	repoID string,
	results []map[string]any,
) (map[string]searchdocs.Document, []string) {
	docByID := make(map[string]searchdocs.Document, len(results))
	order := make([]string, 0, len(results))
	for _, result := range results {
		entityID := mapString(result, "entity_id")
		if entityID == "" {
			continue
		}
		if _, seen := docByID[entityID]; seen {
			continue
		}
		doc, decision := searchdocs.ProjectContentEntity(searchdocs.ContentEntity{
			EntityID:     entityID,
			RepoID:       resultRepoID(result, repoID),
			RelativePath: mapString(result, "file_path"),
			EntityType:   mapString(result, "entity_type"),
			EntityName:   firstNonEmpty(mapString(result, "entity_name"), mapString(result, "name")),
			Language:     mapString(result, "language"),
			SourceCache:  mapString(result, "source_cache"),
		})
		if !decision.Include {
			continue
		}
		docByID[entityID] = doc
		order = append(order, entityID)
	}
	return docByID, order
}

// reorderCodeResultsByCandidates places result rows in fused-rank order. Rows
// whose entity id did not appear in the ranked candidates keep their original
// relative order and follow the ranked rows, so no lexical result is dropped.
func reorderCodeResultsByCandidates(
	results []map[string]any,
	candidates []searchretrieval.Candidate,
) []map[string]any {
	rank := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		entityID := entityIDFromDocument(candidate.Document)
		if entityID == "" {
			continue
		}
		if _, seen := rank[entityID]; seen {
			continue
		}
		rank[entityID] = len(rank)
	}

	ranked := make([]map[string]any, 0, len(results))
	unranked := make([]map[string]any, 0, len(results))
	placed := make(map[string]struct{}, len(results))
	for _, result := range results {
		entityID := mapString(result, "entity_id")
		if _, ok := rank[entityID]; ok {
			if _, done := placed[entityID]; !done {
				ranked = append(ranked, result)
				placed[entityID] = struct{}{}
				continue
			}
		}
		unranked = append(unranked, result)
	}

	sortByHybridRank(ranked, rank)
	for i := range ranked {
		ranked[i]["search_backend"] = "hybrid"
	}
	return append(ranked, unranked...)
}

// sortByHybridRank stably orders ranked rows by their fused-rank position.
func sortByHybridRank(ranked []map[string]any, rank map[string]int) {
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0; j-- {
			if rank[mapString(ranked[j], "entity_id")] >= rank[mapString(ranked[j-1], "entity_id")] {
				break
			}
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
}

// entityIDFromDocument recovers the content entity id from a projected document.
// ProjectContentEntity attaches a stable "content_entity" graph handle carrying
// the original entity id, so the ranked candidate maps back to its result row.
func entityIDFromDocument(doc searchdocs.Document) string {
	for _, handle := range doc.GraphHandles {
		if handle.Kind == "content_entity" {
			return handle.ID
		}
	}
	for _, ref := range doc.EntityRefs {
		if ref.ID != "" {
			return ref.ID
		}
	}
	return ""
}

func resultRepoID(result map[string]any, fallback string) string {
	if repoID := mapString(result, "repo_id"); repoID != "" {
		return repoID
	}
	return fallback
}

func mapString(values map[string]any, key string) string {
	if raw, ok := values[key].(string); ok {
		return raw
	}
	return ""
}
