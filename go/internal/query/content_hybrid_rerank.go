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

// contentHybridRerankLimit caps the candidate set the in-process hybrid index
// ranks for one content-search request. The handler already bounds its lexical
// result set to the request limit (default 50, hard maximum 200), so the index
// is built over a small, request-local document set and never the whole corpus.
const contentHybridRerankLimit = 200

// contentHybridRerankTimeout bounds the in-process re-rank pass. It guards the
// embedder and cosine scoring against a pathological corpus; the work is
// CPU-only over at most contentHybridRerankLimit request-local documents.
const contentHybridRerankTimeout = 2 * time.Second

// ContentResultReranker reorders bounded content-search result rows by hybrid
// relevance for the search_entity_content and search_file_content tools.
//
// Each method returns the reordered rows and reports whether the hybrid pass
// changed the ranking input. When a method returns applied=false the caller MUST
// keep the lexical ordering and the content_index truth basis: the ranker found
// no in-scope signal to fuse (disabled, empty input, fewer than two rows, no
// query-term overlap, no projectable documents, or an unavailable embedder).
type ContentResultReranker interface {
	RerankEntities(ctx context.Context, repoID, query string, rows []EntityContent) ([]EntityContent, bool)
	RerankFiles(ctx context.Context, repoID, query string, rows []FileContent) ([]FileContent, bool)
}

// ContentHybridRanker reorders lexical content-search results by hybrid
// relevance.
//
// It fuses BM25 lexical scoring with vector cosine similarity (Reciprocal Rank
// Fusion) over the rows a content-search request already retrieved and
// authorized. The ranker never widens the candidate set, issues no graph or
// Postgres reads, and produces no canonical truth: it only changes the order of
// results the lexical path already returned.
//
// Embedding is deliberately confined to a process-local deterministic hash
// embedder, identical to find_code's CodeHybridRanker. This bounded in-request
// re-rank MUST NOT call a governed provider embedder, because that would POST
// request-local source snippets to an external endpoint and block on the
// provider HTTP timeout per result, ignoring client cancellation and bypassing
// the semantic policy / document-vector readiness path. The local embedder keeps
// all source text inside the process.
type ContentHybridRanker struct {
	// enabled gates the re-rank pass; it is true only when the runtime's
	// semantic search is configured.
	enabled bool
	// localEmbedder is the deterministic no-network embedder built once at
	// construction. It is the only embedder the re-rank ever uses, and it is not
	// injectable, so a governed provider embedder can never embed source text on
	// this path.
	localEmbedder searchhybrid.Embedder
}

// NewContentHybridRanker builds a content-search hybrid re-ranker. It owns a
// deterministic local hash embedder; no provider embedder is accepted, so source
// text never egresses on this path. When enabled is false, or the local embedder
// cannot be built, both Rerank methods are no-ops and the caller keeps the
// lexical order.
func NewContentHybridRanker(enabled bool) *ContentHybridRanker {
	ranker := &ContentHybridRanker{enabled: enabled}
	if !enabled {
		return ranker
	}
	if embedder, err := searchembed.NewHashEmbedder(searchembed.DefaultDimensions); err == nil {
		ranker.localEmbedder = embedder
	}
	return ranker
}

// RerankEntities reorders entity-content rows by fused BM25+vector rank.
//
// The candidate documents are projected from the lexical rows with the same
// curated search-document projection the persisted index uses, embedded with the
// shipped local embedder, and ranked in hybrid mode. Rows that do not project
// into a scoped document keep their lexical position appended after the ranked
// set, so the response never drops a row the lexical path returned. Reordered
// rows carry SearchBackend="hybrid"; on applied=false the rows are returned
// untouched.
func (r *ContentHybridRanker) RerankEntities(
	ctx context.Context,
	repoID string,
	query string,
	rows []EntityContent,
) ([]EntityContent, bool) {
	if !r.canRerank(ctx, repoID, len(rows)) {
		return rows, false
	}

	docByID, order := projectEntityContentDocuments(repoID, rows)
	if len(docByID) < 2 {
		return rows, false
	}

	candidates, ok := r.rankDocuments(ctx, repoID, query, docByID, order)
	if !ok {
		return rows, false
	}

	return reorderEntityRowsByCandidates(rows, candidates), true
}

// RerankFiles reorders file-content rows by fused BM25+vector rank. It mirrors
// RerankEntities over content_files projections; see RerankEntities for the
// fallback and truth-basis contract.
func (r *ContentHybridRanker) RerankFiles(
	ctx context.Context,
	repoID string,
	query string,
	rows []FileContent,
) ([]FileContent, bool) {
	if !r.canRerank(ctx, repoID, len(rows)) {
		return rows, false
	}

	docByID, order := projectFileContentDocuments(repoID, rows)
	if len(docByID) < 2 {
		return rows, false
	}

	candidates, ok := r.rankDocuments(ctx, repoID, query, docByID, order)
	if !ok {
		return rows, false
	}

	return reorderFileRowsByCandidates(rows, candidates), true
}

// canRerank reports whether the bounded re-rank may run for this request. It
// guards the disabled, missing-embedder, missing-scope, too-few-rows, and
// already-cancelled edges that make the pass a deterministic no-op.
func (r *ContentHybridRanker) canRerank(ctx context.Context, repoID string, rowCount int) bool {
	if r == nil || !r.enabled || r.localEmbedder == nil || repoID == "" || rowCount < 2 {
		return false
	}
	return ctx.Err() == nil
}

// rankDocuments builds the request-local hybrid index over the projected
// documents and returns the fused candidates. It returns ok=false on any bounded
// failure (index build, request validation, search error, or empty result) so
// callers fall back to the lexical order.
func (r *ContentHybridRanker) rankDocuments(
	ctx context.Context,
	repoID string,
	query string,
	docByID map[string]searchdocs.Document,
	order []string,
) ([]searchretrieval.Candidate, bool) {
	docs := make([]searchdocs.Document, 0, len(docByID))
	for _, id := range order {
		docs = append(docs, docByID[id])
	}

	rankCtx, cancel := context.WithTimeout(ctx, contentHybridRerankTimeout)
	defer cancel()

	// The local hash embedder is deterministic and CPU-only; NewIndex embeds the
	// projected documents here with no network call. The provider embedder is
	// never reachable from this path.
	index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{
		MaxDocuments:    contentHybridRerankLimit,
		Embedder:        r.localEmbedder,
		VectorRetrieval: searchhybrid.VectorRetrievalAuto,
	})
	if err != nil {
		return nil, false
	}

	req := searchretrieval.Request{
		Query:   query,
		Scope:   searchretrieval.Scope{RepoID: repoID},
		Mode:    searchbench.ModeHybrid,
		Limit:   len(docs),
		Timeout: contentHybridRerankTimeout,
	}
	if err := searchretrieval.ValidateRequest(req); err != nil {
		return nil, false
	}

	candidates, err := (searchhybrid.Backend{Index: index}).Search(rankCtx, req)
	if err != nil || len(candidates) == 0 {
		return nil, false
	}
	return candidates, true
}

// projectEntityContentDocuments builds curated search documents keyed by entity
// id from lexical entity rows. It reuses searchdocs.ProjectContentEntity so the
// searchable text and graph handles match the persisted search-document index,
// keeping the in-process re-rank consistent with the durable retrieval lane.
func projectEntityContentDocuments(
	repoID string,
	rows []EntityContent,
) (map[string]searchdocs.Document, []string) {
	docByID := make(map[string]searchdocs.Document, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		entityID := row.EntityID
		if entityID == "" {
			continue
		}
		if _, seen := docByID[entityID]; seen {
			continue
		}
		doc, decision := searchdocs.ProjectContentEntity(searchdocs.ContentEntity{
			EntityID:     entityID,
			RepoID:       firstNonEmpty(row.RepoID, repoID),
			RelativePath: row.RelativePath,
			EntityType:   row.EntityType,
			EntityName:   row.EntityName,
			StartLine:    row.StartLine,
			EndLine:      row.EndLine,
			Language:     row.Language,
			SourceCache:  row.SourceCache,
		})
		if !decision.Include {
			continue
		}
		docByID[entityID] = doc
		order = append(order, entityID)
	}
	return docByID, order
}

// projectFileContentDocuments builds curated search documents keyed by the
// repo-scoped file id from lexical file rows. It reuses
// searchdocs.ProjectContentFile so the searchable text and file handle match the
// persisted search-document index.
//
// When no row carries a non-empty Content body the function returns zero
// projectable documents, causing RerankFiles to fall back to the lexical order
// with no search_backend label. This guards against the production SQL paths
// that intentionally omit file bodies from search results: projecting on path
// and title alone would reorder results by path-hash noise and mislabel them
// search_backend=hybrid instead of preserving the content_index truth basis.
func projectFileContentDocuments(
	repoID string,
	rows []FileContent,
) (map[string]searchdocs.Document, []string) {
	// Require at least one row with a populated body before building a hybrid
	// index. The production file-search SQL paths return Content="" for every
	// row; ranking on path/title/labels alone produces noise-ordered results
	// that must never be labelled hybrid.
	anyBody := false
	for _, row := range rows {
		if row.Content != "" {
			anyBody = true
			break
		}
	}
	if !anyBody {
		return nil, nil
	}

	docByID := make(map[string]searchdocs.Document, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		rowRepoID := firstNonEmpty(row.RepoID, repoID)
		fileID := fileContentDocumentID(rowRepoID, row.RelativePath)
		if fileID == "" {
			continue
		}
		if _, seen := docByID[fileID]; seen {
			continue
		}
		doc, decision := searchdocs.ProjectContentFile(searchdocs.ContentFile{
			RepoID:       rowRepoID,
			RelativePath: row.RelativePath,
			Language:     row.Language,
			ArtifactType: row.ArtifactType,
			Content:      row.Content,
		})
		if !decision.Include {
			continue
		}
		docByID[fileID] = doc
		order = append(order, fileID)
	}
	return docByID, order
}

// reorderEntityRowsByCandidates places entity rows in fused-rank order. Rows
// whose entity id did not appear in the ranked candidates keep their original
// relative order and follow the ranked rows, so no lexical result is dropped.
// Ranked rows carry SearchBackend="hybrid".
func reorderEntityRowsByCandidates(
	rows []EntityContent,
	candidates []searchretrieval.Candidate,
) []EntityContent {
	rank := candidateRankByID(candidates, entityIDFromDocument)

	ranked := make([]EntityContent, 0, len(rows))
	unranked := make([]EntityContent, 0, len(rows))
	placed := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := rank[row.EntityID]; ok {
			if _, done := placed[row.EntityID]; !done {
				placed[row.EntityID] = struct{}{}
				row.SearchBackend = "hybrid"
				ranked = append(ranked, row)
				continue
			}
		}
		unranked = append(unranked, row)
	}

	sortEntityRowsByRank(ranked, rank)
	return append(ranked, unranked...)
}

// reorderFileRowsByCandidates places file rows in fused-rank order, mirroring
// reorderEntityRowsByCandidates over the repo-scoped file id. Ranked rows carry
// SearchBackend="hybrid"; unranked rows keep their lexical order and basis.
func reorderFileRowsByCandidates(
	rows []FileContent,
	candidates []searchretrieval.Candidate,
) []FileContent {
	rank := candidateRankByID(candidates, fileIDFromDocument)

	ranked := make([]FileContent, 0, len(rows))
	unranked := make([]FileContent, 0, len(rows))
	placed := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		fileID := fileContentDocumentID(row.RepoID, row.RelativePath)
		if _, ok := rank[fileID]; ok {
			if _, done := placed[fileID]; !done {
				placed[fileID] = struct{}{}
				row.SearchBackend = "hybrid"
				ranked = append(ranked, row)
				continue
			}
		}
		unranked = append(unranked, row)
	}

	sortFileRowsByRank(ranked, rank)
	return append(ranked, unranked...)
}

// candidateRankByID builds a stable id->rank map from fused candidates using the
// supplied id extractor, skipping documents with no recoverable id and keeping
// the first occurrence of each id.
func candidateRankByID(
	candidates []searchretrieval.Candidate,
	idOf func(searchdocs.Document) string,
) map[string]int {
	rank := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		id := idOf(candidate.Document)
		if id == "" {
			continue
		}
		if _, seen := rank[id]; seen {
			continue
		}
		rank[id] = len(rank)
	}
	return rank
}

// sortEntityRowsByRank stably orders ranked entity rows by their fused-rank
// position using an insertion sort, matching find_code's stable re-rank.
func sortEntityRowsByRank(ranked []EntityContent, rank map[string]int) {
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0; j-- {
			if rank[ranked[j].EntityID] >= rank[ranked[j-1].EntityID] {
				break
			}
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
}

// sortFileRowsByRank stably orders ranked file rows by their fused-rank position.
func sortFileRowsByRank(ranked []FileContent, rank map[string]int) {
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0; j-- {
			if rank[fileContentDocumentID(ranked[j].RepoID, ranked[j].RelativePath)] >=
				rank[fileContentDocumentID(ranked[j-1].RepoID, ranked[j-1].RelativePath)] {
				break
			}
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
}

// fileIDFromDocument recovers the repo-scoped file id from a projected file
// document. ProjectContentFile attaches a stable "file" graph handle carrying
// the "<repoID>:<relativePath>" id, so the ranked candidate maps back to its row.
func fileIDFromDocument(doc searchdocs.Document) string {
	for _, handle := range doc.GraphHandles {
		if handle.Kind == "file" {
			return handle.ID
		}
	}
	return ""
}

// fileContentDocumentID returns the stable repo-scoped file id used as the
// projection key and rank key. It matches the id ProjectContentFile assigns to
// the "file" graph handle, so rows and ranked candidates align.
func fileContentDocumentID(repoID, relativePath string) string {
	if repoID == "" || relativePath == "" {
		return ""
	}
	return repoID + ":" + relativePath
}
