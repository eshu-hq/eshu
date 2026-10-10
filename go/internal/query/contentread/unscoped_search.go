// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// errCursorNeedsUnscopedSearch rejects a resume cursor on any search the
// bounded walk does not serve, so a client never believes a cursor was honored.
var errCursorNeedsUnscopedSearch = errors.New("cursor is only supported by a file search with no repository filter")

// unscopedFileSearcher returns the bounded unscoped seam when the request
// carries no repository filter (explicit or injected from a scoped token's
// grant) and the store implements it.
func (h *ContentHandler) unscopedFileSearcher(req contentSearchRequest) (querycontract.UnscopedFileSearcher, bool) {
	if req.repoID() != "" || len(req.explicitRepoIDs()) > 0 {
		return nil, false
	}
	searcher, ok := h.Content.(querycontract.UnscopedFileSearcher)
	return searcher, ok
}

// searchFilesUnscoped answers a file search that has no repository filter from
// the bounded walk (#7730). A page the budget cut short is HTTP 200 with
// truncated=true, data.partial, and truth level partial: it is never an error
// and never reads as complete.
func (h *ContentHandler) searchFilesUnscoped(
	w http.ResponseWriter,
	r *http.Request,
	req contentSearchRequest,
	searcher querycontract.UnscopedFileSearcher,
) {
	ctx := r.Context()
	if querycontract.RepositoryAccessFilterFromContext(ctx).Empty() {
		querycontract.WriteSuccess(w, r, http.StatusOK, contentSearchResponse([]querycontract.FileContent{}, req, false), h.fileSearchTruth())
		return
	}
	cursor := req.cursor()
	page, err := searcher.SearchFilesUnscoped(ctx, req.pattern(), req.limit(), req.offset(), cursor.RepoID, cursor.RelativePath)
	if err != nil {
		h.writeFileSearchError(w, r, err)
		return
	}

	results := h.rerankFileResults(ctx, req, page.Files)
	truth := h.fileSearchTruth()
	body := contentSearchResponse(results, req, page.More || page.Partial != nil)
	if page.Partial != nil {
		body["partial"] = page.Partial
		truth.Level = querycontract.MinTruthLevel(truth.Level, querycontract.TruthLevelPartial)
		truth.Truncated = true
		truth.Reason = page.Partial.Reason
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, truth)
}

func (h *ContentHandler) fileSearchTruth() *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex, "resolved from bounded file content search")
}

// writeFileSearchError answers a failed file search in the shared order: the
// substring-index readiness 503, the reader-fence 503, the unsupported-paging
// 400, then the fixed-message 500.
func (h *ContentHandler) writeFileSearchError(w http.ResponseWriter, r *http.Request, err error) {
	if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
		return
	}
	if querycontract.WriteGraphReadError(w, r, err, "code_search.content_search") {
		return
	}
	if errors.Is(err, errUnsupportedPagedFileSearch) {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, contentFileSearchFailedMessage)
}
