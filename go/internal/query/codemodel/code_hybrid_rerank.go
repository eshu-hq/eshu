// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// codeHybridRerankLimit caps the candidate set the in-process hybrid index ranks
// for one find_code request. It equals CodeSearchRankedWindow, the largest page
// the handler can pass in (default 50, maximum 200 rows), so a maximum page is
// ranked in full and the index is still built over a request-local document set,
// never the whole corpus. Rows past the cap, by entity id, would be dropped as
// index overflow and kept in lexical order after the ranked rows (#7746).
const codeHybridRerankLimit = CodeSearchRankedWindow

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
	// LocalEmbedder is the deterministic no-network embedder built once at
	// construction. It is the only embedder the re-rank ever uses, and it is
	// not injectable, so a governed provider embedder can never embed source
	// text on this path. It is exported because the staying no-egress test
	// asserts its concrete type via the root alias.
	LocalEmbedder searchhybrid.Embedder
	// tracer records the query.hybrid_rerank stage span. Nil means the shared
	// query tracer; in-package tests set a recording tracer here instead of
	// swapping a package-level var.
	tracer trace.Tracer
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
		ranker.LocalEmbedder = embedder
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
//
// A pass that reaches the ranking work records one query.hybrid_rerank span
// (tool find_code, the page row count, and whether it applied), so an operator
// can see the stage's share of a slow request.
func (r *CodeHybridRanker) Rerank(
	ctx context.Context,
	repoID string,
	query string,
	results []map[string]any,
) ([]map[string]any, bool) {
	if r == nil || !r.enabled || r.LocalEmbedder == nil || repoID == "" || len(results) < 2 {
		return results, false
	}
	if err := ctx.Err(); err != nil {
		return results, false
	}

	ctx, finish := tracing.StartHybridRerankSpan(ctx, r.stageTracer(), tracing.HybridRerankToolFindCode, len(results))
	reranked, applied := r.rerank(ctx, repoID, query, results)
	finish(applied)
	return reranked, applied
}

// stageTracer returns the tracer for the re-rank stage span: the ranker's own
// when a test set one, otherwise the shared query tracer.
func (r *CodeHybridRanker) stageTracer() trace.Tracer {
	if r.tracer != nil {
		return r.tracer
	}
	return tracing.HandlerTracer()
}

// rerank projects, indexes, and ranks one page. It returns the rows unchanged
// with applied=false on any bounded failure, so the caller keeps lexical order.
func (r *CodeHybridRanker) rerank(
	ctx context.Context,
	repoID string,
	query string,
	results []map[string]any,
) ([]map[string]any, bool) {
	docByID, order := projectCodeResultDocuments(repoID, results)
	if len(docByID) < 2 {
		return results, false
	}
	candidates, err := r.rankDocuments(ctx, repoID, query, docByID, order)
	if err != nil || len(candidates) == 0 {
		return results, false
	}
	return reorderCodeResultsByCandidates(results, candidates), true
}

// rankDocuments builds the request-local hybrid index over the projected
// documents and ranks all of them. It returns the index-build or ranking
// error (for example the codeHybridRerankTimeout deadline) unchanged.
func (r *CodeHybridRanker) rankDocuments(
	ctx context.Context,
	repoID string,
	query string,
	docByID map[string]searchdocs.Document,
	order []string,
) ([]searchretrieval.Candidate, error) {
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
		Embedder:        r.LocalEmbedder,
		VectorRetrieval: searchhybrid.VectorRetrievalAuto,
	})
	if err != nil {
		return nil, err
	}

	// RankAll ranks the whole request-local index. Search would reject a page
	// larger than the public retrieval limit (100) and leave it in lexical
	// order (#7746); this page is already bounded by codeHybridRerankLimit.
	scope := searchretrieval.Scope{RepoID: repoID}
	return (searchhybrid.Backend{Index: index}).RankAll(rankCtx, query, scope)
}

// CodeRankerIdentity names the find_code ranker in a rank-walk basis. A
// change to the fusion, the embedder, or its dimensions must change it, so a
// walk started on the old ranker answers 409 instead of mixing two orders.
var CodeRankerIdentity = fmt.Sprintf("find_code_hybrid/rrf/hash_embedder_%d/v1", searchembed.DefaultDimensions)

// CodeWindowScorer scores a whole find_code rank-walk window (#7746). The
// find_code handler requires it of its HybridRanker to serve rank_cursor; a
// ranker without it, or one whose CanScoreWindow is false, answers
// rank_cursor_unsupported (ranker_disabled), never a silent offset page.
type CodeWindowScorer interface {
	// CanScoreWindow reports whether the ranker is on, so a disabled ranker
	// is refused before the window is read.
	CanScoreWindow() bool
	// ScoreCodeWindow returns the fused score of every ranked row keyed by
	// entity_id, or the error that stopped scoring. It never returns a
	// partial map.
	ScoreCodeWindow(ctx context.Context, repoID, query string, rows []map[string]any) (map[string]float64, error)
}

var _ CodeWindowScorer = (*CodeHybridRanker)(nil)

// CanScoreWindow reports whether semantic search is on and the local
// embedder was built.
func (r *CodeHybridRanker) CanScoreWindow() bool {
	return r != nil && r.enabled && r.LocalEmbedder != nil
}

// ScoreCodeWindow returns the fused BM25+vector score of every projectable
// window row, keyed by entity_id. Rows that do not project, or that the
// ranker does not return, have no score; the walk orders them after the
// scored rows. A pass over two or more rows records one query.hybrid_rerank
// span (tool find_code) whose search.rerank.rows is the window size.
func (r *CodeHybridRanker) ScoreCodeWindow(
	ctx context.Context,
	repoID string,
	query string,
	rows []map[string]any,
) (map[string]float64, error) {
	if !r.CanScoreWindow() {
		return nil, errors.New("find_code hybrid ranker is disabled")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scores := make(map[string]float64, len(rows))
	if len(rows) < 2 {
		return scores, nil
	}
	ctx, finish := tracing.StartHybridRerankSpan(ctx, r.stageTracer(), tracing.HybridRerankToolFindCode, len(rows))
	applied := false
	defer func() { finish(applied) }()

	docByID, order := projectCodeResultDocuments(repoID, rows)
	if len(docByID) == 0 {
		return scores, nil
	}
	candidates, err := r.rankDocuments(ctx, repoID, query, docByID, order)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		id := EntityIDFromDocument(candidate.Document)
		if _, seen := scores[id]; id != "" && !seen {
			scores[id] = candidate.Score
		}
	}
	applied = len(scores) > 0
	return scores, nil
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
		entityID := EntityIDFromDocument(candidate.Document)
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

// EntityIDFromDocument recovers the content entity id from a projected document.
// ProjectContentEntity attaches a stable "content_entity" graph handle carrying
// the original entity id, so the ranked candidate maps back to its result row.
// EntityIDFromDocument returns the entity ID carried by a hybrid document.
// It is exported because the staying content-hybrid lane passes it as a
// rank function through the root forward.
func EntityIDFromDocument(doc searchdocs.Document) string {
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

// firstNonEmpty is a family-local copy of root's
// repository_deployment_evidence_read_model.go helper of the same name. The
// hybrid ranker picks the entity display name through it, and the staying
// deployment-evidence readers that share it cannot cross the package
// boundary, so the leaf carries this byte-identical copy instead of
// importing root. Keep it behavior-identical to its root source.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
