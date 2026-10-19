// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
)

// contentRankerIdentity names the ranker in the walk basis. A change to the
// fusion, the embedder, or its dimensions must change this string so a walk
// started on the old ranker answers 409 instead of mixing two orders.
var contentRankerIdentity = fmt.Sprintf("content_hybrid/rrf/hash_embedder_%d/v1", searchembed.DefaultDimensions)

// errRankWindowDisabled reports a ranker that cannot score a walk window:
// semantic search is off, or the local embedder could not be built.
var errRankWindowDisabled = errors.New("hybrid ranker disabled")

// EntityWindowScorer scores a whole rank-walk window of entity rows. The
// handler requires it of HybridRanker to serve rank_cursor; a ranker that
// does not implement it answers rank_cursor_unsupported (ranker_disabled),
// never a silent offset page.
type EntityWindowScorer interface {
	// CanScoreWindow reports whether the ranker is on, so a disabled ranker
	// is refused before the window is read.
	CanScoreWindow() bool
	ScoreEntityWindow(ctx context.Context, repoID, query string, rows []querycontract.EntityContent) (map[string]float64, error)
}

var _ EntityWindowScorer = (*ContentHybridRanker)(nil)

// CanScoreWindow reports whether semantic search is on and the local
// embedder was built.
func (r *ContentHybridRanker) CanScoreWindow() bool {
	return r != nil && r.enabled && r.localEmbedder != nil
}

// ScoreEntityWindow returns the fused BM25+vector score of every projectable
// row, keyed by entity id. Rows that do not project, or that the ranker does
// not return, have no score and walk after the scored rows. It returns
// errRankWindowDisabled when the ranker is off, and the ranking error (a
// timeout or cancel) when scoring did not finish; a partial score map is
// never returned. A pass over two or more rows records one
// query.hybrid_rerank span with search.rerank.rows equal to the window size.
func (r *ContentHybridRanker) ScoreEntityWindow(
	ctx context.Context,
	repoID string,
	query string,
	rows []querycontract.EntityContent,
) (map[string]float64, error) {
	if !r.CanScoreWindow() {
		return nil, errRankWindowDisabled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scores := make(map[string]float64, len(rows))
	if len(rows) < 2 {
		return scores, nil
	}
	// A walk re-reads its whole window on every page (#8216). The scores
	// are a deterministic function of the key below, so a repeat window
	// skips the projection, index build, and ranking, and rank.Walk still
	// checks the live window basis and order: exactly-once and the 409
	// behavior are unchanged. A hit emits no query.hybrid_rerank span; a
	// served page without that span is the operator-visible hit signal.
	key := codemodel.KeyWindowScores(contentRankerIdentity, repoID, query, entityWindowScoreRows(repoID, rows))
	if cached, ok := r.windowScores.Get(key); ok {
		return cached, nil
	}
	ctx, finish := tracing.StartHybridRerankSpan(ctx, r.stageTracer(), tracing.HybridRerankToolSearchEntityContent, len(rows))
	applied := false
	defer func() { finish(applied) }()

	docByID, order := projectEntityContentDocuments(repoID, rows)
	if len(docByID) == 0 {
		r.windowScores.Put(key, scores)
		return scores, nil
	}
	candidates, err := r.rankDocumentsErr(ctx, repoID, query, docByID, order)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		id := codemodel.EntityIDFromDocument(candidate.Document)
		if _, seen := scores[id]; id != "" && !seen {
			scores[id] = candidate.Score
		}
	}
	applied = rank.AnyScored(scores)
	r.windowScores.Put(key, scores)
	return scores, nil
}

// entityWindowScoreRows maps entity rows to the cache key fields
// ScoreEntityWindow scores by. It must cover every field
// projectEntityContentDocuments reads (entity id, repo id, relative path,
// entity type and name, start and end lines, language, source cache): a
// field the projection reads but the key misses would serve a stale score
// after an edit rank.Walk would otherwise refuse with 409 basis_changed.
func entityWindowScoreRows(repoID string, rows []querycontract.EntityContent) []codemodel.WindowScoreRow {
	keyed := make([]codemodel.WindowScoreRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.RepoID) == "" {
			row.RepoID = repoID
		}
		keyed = append(keyed, codemodel.WindowScoreRow{
			EntityID:   row.EntityID,
			RepoID:     row.RepoID,
			Path:       row.RelativePath,
			EntityType: row.EntityType,
			EntityName: row.EntityName,
			Language:   row.Language,
			StartLine:  row.StartLine,
			EndLine:    row.EndLine,
			Body:       row.SourceCache,
		})
	}
	return keyed
}

// writeRankError records the rank-walk outcome for err and answers with it.
// rank.Answer writes nothing for a *rank.Error whose code rank.WriteError
// does not answer; that falls through to the fixed rank failure 500 (499 for
// a client cancel), the same answer the walk's own failure branch gives, so
// the request never ends without a body.
func writeRankError(w http.ResponseWriter, r *http.Request, tool string, err *rank.Error) {
	if rank.Answer(w, r, tool, err) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, contentEntityRankFailedMessage)
}

// searchEntitiesRanked serves search_entity_content as a relevance walk over
// the first rank.WindowSize lexical rows (#7746). Every request rebuilds and
// re-scores the window, so the cursor carries no rows; rank.Walk refuses a
// cursor whose window or served rows no longer match.
func (h *ContentHandler) searchEntitiesRanked(w http.ResponseWriter, r *http.Request, req contentSearchRequest) {
	const tool = rank.ToolSearchEntityContent
	if req.offset() > 0 {
		writeRankError(w, r, tool, rank.Unsupported(rank.ReasonOffsetConflict))
		return
	}
	if req.Cursor != nil {
		writeRankError(w, r, tool, rank.Unsupported(rank.ReasonCursorConflict))
		return
	}
	scorer, ok := h.HybridRanker.(EntityWindowScorer)
	if !ok || !scorer.CanScoreWindow() {
		writeRankError(w, r, tool, rank.Unsupported(rank.ReasonRankerDisabled))
		return
	}
	// The repository must be named in the request: a walk never widens to,
	// or silently narrows from, the caller's grant.
	if req.RepoID == "" && len(req.explicitRepoIDs()) != 1 {
		writeRankError(w, r, tool, rank.Unsupported(rank.ReasonScopeNotSingleRepo))
		return
	}
	req, err := h.normalizeContentSearchRequest(r.Context(), req)
	if err != nil {
		writeContentSelectorError(w, r, err, "code_search.content_search")
		return
	}
	repoID := req.repoID()

	windowReq := req
	windowReq.RepoID, windowReq.RepoIDs = repoID, nil
	windowReq.Limit, windowReq.Offset = rank.WindowSize, 0
	rows, lexicalTruncated, err := h.searchEntitiesByScope(r.Context(), windowReq)
	if err != nil {
		writeEntitySearchError(w, r, err)
		return
	}
	scores, err := scorer.ScoreEntityWindow(r.Context(), repoID, req.pattern(), rows)
	if errors.Is(err, errRankWindowDisabled) {
		writeRankError(w, r, tool, rank.Unsupported(rank.ReasonRankerDisabled))
		return
	}
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, contentEntityRankFailedMessage)
		return
	}

	page, err := rank.Walk(entityWindow(repoID, req.pattern(), rows, scores, lexicalTruncated), scores, req.RankCursor, req.limit())
	if rank.Answer(w, r, tool, err) {
		return
	}
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, contentEntityRankFailedMessage)
		return
	}
	h.writeEntityRankWalkPage(w, r, req, rows, scores, page)
}

// writeEntityRankWalkPage shapes and writes a served walk page. A page id with
// no row is a server fault: it answers the fixed 500 with no row data and
// records no walk outcome, because serving a short page would lose the row.
func (h *ContentHandler) writeEntityRankWalkPage(
	w http.ResponseWriter, r *http.Request, req contentSearchRequest,
	rows []querycontract.EntityContent, scores map[string]float64, page rank.Page,
) {
	pageRows, err := entityPageRows(rows, scores, page.IDs)
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, contentEntityRankFailedMessage)
		return
	}
	rank.RecordPage(r.Context(), rank.ToolSearchEntityContent, page)
	response := entityContentSearchResponse(pageRows, req, !page.WindowEnd || page.LexicalTruncated)
	response[rank.BlockKey] = page.Block()
	querycontract.WriteSuccess(w, r, http.StatusOK, response, querycontract.BuildTruthEnvelope(
		h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex,
		"resolved from bounded entity content search, walked in hybrid BM25+vector relevance order over the first 200 lexical rows",
	))
}

// entityWindow builds the walk window from the lexical rows. Each row's
// content covers every field the ranker projects, so an edit between pages
// changes the basis. The order is hybrid when at least one row is
// rank.Scored (a finite score), else lexical (window order); the ranker identity enters the basis only for
// a hybrid order.
func entityWindow(repoID, pattern string, rows []querycontract.EntityContent, scores map[string]float64, lexicalTruncated bool) rank.Window {
	order, ranker := rank.OrderLexical, ""
	if rank.AnyScored(scores) {
		order, ranker = rank.OrderHybrid, contentRankerIdentity
	}
	window := rank.Window{
		Tool:             rank.ToolSearchEntityContent,
		Lane:             rank.LaneContent,
		Order:            order,
		Scope:            []string{repoID, pattern},
		Ranker:           ranker,
		Rows:             make([]rank.Row, 0, len(rows)),
		LexicalTruncated: lexicalTruncated,
	}
	for _, row := range rows {
		window.Rows = append(window.Rows, rank.Row{ID: row.EntityID, Content: strings.Join([]string{
			row.RepoID, row.RelativePath, row.EntityType, row.EntityName,
			strconv.Itoa(row.StartLine), strconv.Itoa(row.EndLine), row.Language, row.SourceCache,
		}, "\x00")})
	}
	return window
}

// entityPageRows returns the page's rows in walk order, or
// rank.ErrPageRowMissing and no rows when a page id has no row (the
// rank.MapPage contract). A rank.Scored row carries SearchBackend="hybrid"; an unscored row keeps the lexical basis. No row
// carries page_position: a walk page is not a window of the offset order.
func entityPageRows(rows []querycontract.EntityContent, scores map[string]float64, ids []string) ([]querycontract.EntityContent, error) {
	byID := make(map[string]querycontract.EntityContent, len(rows))
	for _, row := range rows {
		if _, seen := byID[row.EntityID]; !seen {
			byID[row.EntityID] = row
		}
	}
	page := make([]querycontract.EntityContent, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %q", rank.ErrPageRowMissing, id)
		}
		row.PagePosition = nil
		if rank.Scored(scores, id) {
			row.SearchBackend = "hybrid"
		}
		page = append(page, row)
	}
	return page, nil
}
