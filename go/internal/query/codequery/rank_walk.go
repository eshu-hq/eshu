// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// codeRankWalkRequest is the find_code request a rank walk serves.
type codeRankWalkRequest struct {
	query, repoID, language, searchType, token string
	limit, offset                              int
	exact                                      bool
}

// codeRankWalkContentKeys are the row fields the hybrid ranker projects and
// the graph lane returns; they enter the walk basis so an edit between pages
// answers 409 basis_changed.
var codeRankWalkContentKeys = []string{
	"repo_id", "file_path", "entity_type", "entity_name", "name", "language", "source_cache", "start_line", "end_line",
}

// selectCodeSearchLane is the one lane choice of a repository-selected
// find_code walk (#7746, fork-6 ruling): the graph lane first, as an offset
// request reads it; the content lane when the graph lane returns no rows or
// no graph is configured. It returns the lane that answered or failed, so the
// caller can pick the failure message.
func (h *CodeHandler) selectCodeSearchLane(ctx context.Context, req codeRankWalkRequest, probeLimit int) (string, []map[string]any, error) {
	if h.Neo4j != nil {
		rows, err := h.searchGraphEntitiesWithExact(ctx, req.repoID, req.query, req.language, probeLimit, req.exact)
		if err != nil || len(rows) > 0 {
			return rank.LaneGraph, rows, err
		}
	}
	rows, err := h.searchEntityContentWithExact(ctx, req.repoID, req.query, req.language, probeLimit, req.exact)
	return rank.LaneContent, rows, err
}

// handleRankWalk serves find_code as a walk over the first rank.WindowSize
// rows of the lane selectCodeSearchLane picks. The graph lane has no ranker
// and keeps its own order (graph_name), so its pages equal the offset pages.
// The content lane is ranked (hybrid) when the ranker can score the window
// and the request is not exact; otherwise it keeps its own order (lexical).
// Each request re-reads the lane and the window under the caller's grant,
// and rank.Walk answers 409 when the lane, the window, or the order changed.
func (h *CodeHandler) handleRankWalk(w http.ResponseWriter, r *http.Request, req codeRankWalkRequest) {
	ctx := r.Context()
	capability := "code_search.fuzzy_symbol"
	if strings.EqualFold(strings.TrimSpace(req.searchType), "variable") {
		capability = "code_search.variable_lookup"
	} else if req.exact {
		capability = "code_search.exact_symbol"
	}
	switch {
	case req.offset > 0:
		rank.Answer(w, r, rank.ToolFindCode, rank.Unsupported(rank.ReasonOffsetConflict))
		return
	case req.repoID == "":
		rank.Answer(w, r, rank.ToolFindCode, rank.Unsupported(rank.ReasonScopeNotSingleRepo))
		return
	}

	lane, rows, err := h.selectCodeSearchLane(ctx, req, rank.WindowSize+1)
	if err != nil {
		if writeCodeUnavailable(w, r, err, ErrContentSubstringIndexesNotReady, capability) {
			return
		}
		message := codeSearchContentFailedMessage
		if lane == rank.LaneGraph {
			message = codeSearchGraphFailedMessage
		}
		writeCodeReadFailure(w, r, err, capability, message)
		return
	}
	rows, lexicalTruncated := codemodel.CodeSearchPageRows(rows, rank.WindowSize, 0)

	window := rank.Window{
		Tool: rank.ToolFindCode, Lane: lane, Order: rank.OrderGraphName,
		Scope:            []string{req.repoID, req.query, req.language, strconv.FormatBool(req.exact)},
		LexicalTruncated: lexicalTruncated,
	}
	var scores map[string]float64
	if lane == rank.LaneContent {
		window.Order = rank.OrderLexical
		scorer, ok := h.HybridRanker.(codemodel.CodeWindowScorer)
		if ok && scorer.CanScoreWindow() && !req.exact {
			if scores, err = scorer.ScoreCodeWindow(ctx, req.repoID, req.query, rows); err != nil {
				writeCodeReadFailure(w, r, err, capability, codeSearchRankFailedMessage)
				return
			}
		}
		if rank.AnyScored(scores) {
			window.Order, window.Ranker = rank.OrderHybrid, codemodel.CodeRankerIdentity
		}
	}
	windowRows, byID := rank.MapRows(rows, codeRankWalkContentKeys...)
	window.Rows = windowRows
	page, err := rank.Walk(window, scores, req.token, req.limit)
	if rank.Answer(w, r, rank.ToolFindCode, err) {
		return
	}
	if err != nil {
		writeCodeReadFailure(w, r, err, capability, codeSearchRankFailedMessage)
		return
	}
	h.writeRankWalkPage(w, r, req, page, byID, scores, capability)
}

// writeRankWalkPage shapes a walk page like the offset page of its lane and
// adds the rank_walk block. A page id with no row is a server fault: it
// answers the fixed 500 with no row data and records no walk outcome,
// because serving a short page would lose the row.
func (h *CodeHandler) writeRankWalkPage(
	w http.ResponseWriter, r *http.Request, req codeRankWalkRequest, page rank.Page,
	byID map[string]map[string]any, scores map[string]float64, capability string,
) {
	rows, err := rank.MapPage(byID, scores, page.IDs)
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, codeSearchRankFailedMessage)
		return
	}
	rank.RecordPage(r.Context(), rank.ToolFindCode, page)
	truncated := !page.WindowEnd || page.LexicalTruncated
	source, sourceBackend, basis := "content", "postgres_content_store", TruthBasisContentIndex
	detail := "resolved from content index fallback, walked in its own order over the first 200 rows"
	switch {
	case page.Lane == rank.LaneGraph:
		source, sourceBackend, basis = "graph", "graph", TruthBasisAuthoritativeGraph
		detail = "resolved from graph-backed entity search, walked in its own order over the first 200 rows"
	case page.Order == rank.OrderHybrid:
		sourceBackend = "hybrid_content_store"
		detail = "resolved from content index fallback, walked in hybrid BM25+vector relevance order over the first 200 rows"
	}
	payload := codemodel.CodeSearchPagePayloadFromPage(source, sourceBackend, req.query, req.repoID, rows, truncated, req.limit, 0)
	payload[rank.BlockKey] = page.Block()
	WriteSuccess(w, r, http.StatusOK, payload, BuildTruthEnvelope(h.profile(), capability, basis, detail))
}
