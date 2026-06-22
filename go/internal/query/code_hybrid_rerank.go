package query

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
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
// Fusion) over the entities a find_code request already retrieved, using the
// shipped local embedder. The ranker never widens the candidate set, issues no
// graph or Postgres reads, and produces no canonical truth: it only changes the
// order of results the lexical path already authorized and returned.
type CodeHybridRanker struct {
	embedder searchhybrid.Embedder
}

// NewCodeHybridRanker builds a hybrid re-ranker over the given embedder. A nil
// embedder yields a ranker whose Rerank is a no-op, so callers can wire it
// unconditionally and let configuration decide whether vectors participate.
func NewCodeHybridRanker(embedder searchhybrid.Embedder) *CodeHybridRanker {
	return &CodeHybridRanker{embedder: embedder}
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
	if r == nil || r.embedder == nil || repoID == "" || len(results) < 2 {
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

	index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{
		MaxDocuments:    codeHybridRerankLimit,
		Embedder:        r.embedder,
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

	rankCtx, cancel := context.WithTimeout(ctx, codeHybridRerankTimeout)
	defer cancel()
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
