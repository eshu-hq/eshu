// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
)

// Search-page bounds for the content read surface. Exported (#6060) so
// root's OpenAPI sweep test can keep asserting the emitted schema maximum
// against the same constant the handler validates: the offset maximum is
// already wire-public (openapi/paths/search/content.go emits it), so naming it here
// adds no new surface.
const (
	ContentSearchDefaultLimit = 50
	ContentSearchMaxLimit     = 200
	ContentSearchMaxOffset    = 10000
)

var (
	errUnsupportedPagedFileSearch   = errors.New("content store does not support paged file search")
	errUnsupportedPagedEntitySearch = errors.New("content store does not support paged entity search")
)

// ContentHandler serves HTTP endpoints for reading file and entity content
// from the Postgres content store.
type ContentHandler struct {
	Content querycontract.ContentStore
	Profile querycontract.QueryProfile
	// HybridRanker, when set, reorders bounded content-search results by fused
	// BM25+vector relevance over the already-authorized lexical rows. It is gated
	// on the semantic-search embedder being enabled; when nil the lexical
	// content-index order is served unchanged.
	HybridRanker ContentResultReranker
}

func (h *ContentHandler) profile() querycontract.QueryProfile {
	if h == nil {
		return querycontract.ProfileProduction
	}
	return querycontract.NormalizeQueryProfile(string(h.Profile))
}

// Mount registers content query routes on the given mux.
func (h *ContentHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/content/files/read", h.readFile)
	mux.HandleFunc("POST /api/v0/content/files/lines", h.readFileLines)
	mux.HandleFunc("POST /api/v0/content/entities/read", h.readEntity)
	mux.HandleFunc("POST /api/v0/content/files/search", h.searchFiles)
	mux.HandleFunc("POST /api/v0/content/entities/search", h.searchEntities)
}

// readFile reads full file content.
// POST /api/v0/content/files/read
// Body: {"repo_id": "...", "relative_path": "..."}
func (h *ContentHandler) readFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoID       string `json:"repo_id"`
		RelativePath string `json:"relative_path"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.RepoID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if req.RelativePath == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "relative_path is required")
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	resolvedRepoID, err := h.resolveRepositorySelectorForAccess(r.Context(), req.RepoID, access)
	if err != nil {
		writeContentSelectorError(w, err)
		return
	}
	req.RepoID = resolvedRepoID

	fc, err := h.Content.GetFileContent(r.Context(), req.RepoID, req.RelativePath)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fc == nil {
		querycontract.WriteError(w, http.StatusNotFound, "file not found")
		return
	}

	querycontract.WriteSuccess(w, r, http.StatusOK, fc, querycontract.BuildTruthEnvelope(h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex, "resolved from exact file content lookup"))
}

// readFileLines reads a line range from a file.
// POST /api/v0/content/files/lines
// Body: {"repo_id": "...", "relative_path": "...", "start_line": 1, "end_line": 50}
func (h *ContentHandler) readFileLines(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoID       string `json:"repo_id"`
		RelativePath string `json:"relative_path"`
		StartLine    int    `json:"start_line"`
		EndLine      int    `json:"end_line"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.RepoID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if req.RelativePath == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "relative_path is required")
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	resolvedRepoID, err := h.resolveRepositorySelectorForAccess(r.Context(), req.RepoID, access)
	if err != nil {
		writeContentSelectorError(w, err)
		return
	}
	req.RepoID = resolvedRepoID

	fc, err := h.Content.GetFileLines(r.Context(), req.RepoID, req.RelativePath, req.StartLine, req.EndLine)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fc == nil {
		querycontract.WriteError(w, http.StatusNotFound, "file not found")
		return
	}

	querycontract.WriteSuccess(w, r, http.StatusOK, fc, querycontract.BuildTruthEnvelope(h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex, "resolved from exact file line lookup"))
}

// readEntity reads entity content by entity_id.
// POST /api/v0/content/entities/read
// Body: {"entity_id": "..."}
func (h *ContentHandler) readEntity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EntityID string `json:"entity_id"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.EntityID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		querycontract.WriteError(w, http.StatusNotFound, "entity not found")
		return
	}
	ec, err := getEntityContentForRepositoryAccess(r.Context(), h.Content, req.EntityID, access)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ec == nil {
		querycontract.WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	querycontract.WriteSuccess(w, r, http.StatusOK, ec, querycontract.BuildTruthEnvelope(h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex, "resolved from exact entity content lookup"))
}

// searchFiles searches file content by pattern.
// POST /api/v0/content/files/search
// Body: {"repo_id": "...", "query": "...", "limit": 50}
func (h *ContentHandler) searchFiles(w http.ResponseWriter, r *http.Request) {
	req, err := readContentSearchRequest(r)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.validate(); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req, err = h.normalizeContentSearchRequest(r.Context(), req)
	if err != nil {
		writeContentSelectorError(w, err)
		return
	}

	results, truncated, err := h.searchFilesByScope(r.Context(), req)
	if err != nil {
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		if errors.Is(err, errUnsupportedPagedFileSearch) {
			querycontract.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results = h.rerankFileResults(r.Context(), req, results)

	querycontract.WriteSuccess(w, r, http.StatusOK, contentSearchResponse(results, req, truncated), querycontract.BuildTruthEnvelope(h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex, "resolved from bounded file content search"))
}

// searchEntities searches entity source cache by pattern.
// POST /api/v0/content/entities/search
// Body: {"repo_id": "...", "query": "...", "limit": 50}
func (h *ContentHandler) searchEntities(w http.ResponseWriter, r *http.Request) {
	req, err := readContentSearchRequest(r)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.validate(); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req, err = h.normalizeContentSearchRequest(r.Context(), req)
	if err != nil {
		writeContentSelectorError(w, err)
		return
	}

	results, truncated, err := h.searchEntitiesByScope(r.Context(), req)
	if err != nil {
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		if errors.Is(err, errUnsupportedPagedEntitySearch) {
			querycontract.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results = h.rerankEntityResults(r.Context(), req, results)

	querycontract.WriteSuccess(w, r, http.StatusOK, contentSearchResponse(results, req, truncated), querycontract.BuildTruthEnvelope(h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex, "resolved from bounded entity content search"))
}

type contentSearchRequest struct {
	RepoID  string   `json:"repo_id"`
	RepoIDs []string `json:"repo_ids"`
	Query   string   `json:"query"`
	Pattern string   `json:"pattern"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

func readContentSearchRequest(r *http.Request) (contentSearchRequest, error) {
	var req contentSearchRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		return contentSearchRequest{}, err
	}
	return req, nil
}

func (req contentSearchRequest) validate() error {
	if req.pattern() == "" {
		return errors.New("query is required")
	}
	if req.Offset > ContentSearchMaxOffset {
		return fmt.Errorf("offset exceeds maximum of %d", ContentSearchMaxOffset)
	}
	return nil
}

func (req contentSearchRequest) repoID() string {
	if req.RepoID != "" {
		return req.RepoID
	}
	if len(req.RepoIDs) == 1 {
		return req.RepoIDs[0]
	}
	return ""
}

func (req contentSearchRequest) pattern() string {
	if req.Query != "" {
		return req.Query
	}
	return req.Pattern
}

func (req contentSearchRequest) limit() int {
	if req.Limit <= 0 {
		return ContentSearchDefaultLimit
	}
	if req.Limit > ContentSearchMaxLimit {
		return ContentSearchMaxLimit
	}
	return req.Limit
}

func (req contentSearchRequest) offset() int {
	if req.Offset < 0 {
		return 0
	}
	return req.Offset
}

func (req contentSearchRequest) explicitRepoIDs() []string {
	if req.RepoID != "" {
		return nil
	}

	repoIDs := make([]string, 0, len(req.RepoIDs))
	seen := make(map[string]struct{}, len(req.RepoIDs))
	for _, repoID := range req.RepoIDs {
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	return repoIDs
}

func (h *ContentHandler) resolveRepositorySelectorForAccess(
	ctx context.Context,
	selector string,
	access querycontract.RepositoryAccessFilter,
) (string, error) {
	return queryselector.ResolveExactForAccess(ctx, nil, h.Content, selector, access)
}

func (h *ContentHandler) normalizeContentSearchRequest(ctx context.Context, req contentSearchRequest) (contentSearchRequest, error) {
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if req.RepoID != "" {
		repoID, err := h.resolveRepositorySelectorForAccess(ctx, req.RepoID, access)
		if err != nil {
			return contentSearchRequest{}, err
		}
		req.RepoID = repoID
		req.RepoIDs = nil
		return req, nil
	}
	if len(req.RepoIDs) == 0 {
		if access.Scoped() {
			req.RepoIDs = access.RepositorySearchIDs()
		}
		return req, nil
	}
	resolved := make([]string, 0, len(req.RepoIDs))
	seen := make(map[string]struct{}, len(req.RepoIDs))
	for _, selector := range req.RepoIDs {
		repoID, err := h.resolveRepositorySelectorForAccess(ctx, selector, access)
		if err != nil {
			return contentSearchRequest{}, err
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		resolved = append(resolved, repoID)
	}
	req.RepoIDs = resolved
	return req, nil
}

func (h *ContentHandler) searchFilesByScope(ctx context.Context, req contentSearchRequest) ([]querycontract.FileContent, bool, error) {
	if querycontract.RepositoryAccessFilterFromContext(ctx).Empty() {
		return []querycontract.FileContent{}, false, nil
	}
	if searcher, ok := h.Content.(querycontract.PagedContentSearcher); ok {
		results, err := searcher.SearchFiles(ctx, req.repoID(), req.explicitRepoIDs(), req.pattern(), req.limit()+1, req.offset())
		if err != nil {
			return nil, false, err
		}
		return trimFileContentSearchPage(results, req.limit()), len(results) > req.limit(), nil
	}
	if req.offset() > 0 {
		return nil, false, errUnsupportedPagedFileSearch
	}
	probeLimit := req.limit() + 1
	if repoID := req.repoID(); repoID != "" {
		results, err := h.Content.SearchFileContent(ctx, repoID, req.pattern(), probeLimit)
		return trimFileContentSearchPage(results, req.limit()), len(results) > req.limit(), err
	}
	if repoIDs := req.explicitRepoIDs(); len(repoIDs) > 0 {
		results := make([]querycontract.FileContent, 0, probeLimit)
		for _, repoID := range repoIDs {
			remaining := probeLimit - len(results)
			if remaining <= 0 {
				break
			}
			rows, err := h.Content.SearchFileContent(ctx, repoID, req.pattern(), remaining)
			if err != nil {
				return nil, false, err
			}
			results = append(results, rows...)
		}
		return trimFileContentSearchPage(results, req.limit()), len(results) > req.limit(), nil
	}
	results, err := h.Content.SearchFileContentAnyRepo(ctx, req.pattern(), probeLimit)
	return trimFileContentSearchPage(results, req.limit()), len(results) > req.limit(), err
}

func (h *ContentHandler) searchEntitiesByScope(ctx context.Context, req contentSearchRequest) ([]querycontract.EntityContent, bool, error) {
	if querycontract.RepositoryAccessFilterFromContext(ctx).Empty() {
		return []querycontract.EntityContent{}, false, nil
	}
	if searcher, ok := h.Content.(querycontract.PagedContentSearcher); ok {
		results, err := searcher.SearchEntities(ctx, req.repoID(), req.explicitRepoIDs(), req.pattern(), req.limit()+1, req.offset())
		if err != nil {
			return nil, false, err
		}
		return trimEntityContentSearchPage(results, req.limit()), len(results) > req.limit(), nil
	}
	if req.offset() > 0 {
		return nil, false, errUnsupportedPagedEntitySearch
	}
	probeLimit := req.limit() + 1
	if repoID := req.repoID(); repoID != "" {
		results, err := h.Content.SearchEntityContent(ctx, repoID, req.pattern(), probeLimit)
		return trimEntityContentSearchPage(results, req.limit()), len(results) > req.limit(), err
	}
	if repoIDs := req.explicitRepoIDs(); len(repoIDs) > 0 {
		results := make([]querycontract.EntityContent, 0, probeLimit)
		for _, repoID := range repoIDs {
			remaining := probeLimit - len(results)
			if remaining <= 0 {
				break
			}
			rows, err := h.Content.SearchEntityContent(ctx, repoID, req.pattern(), remaining)
			if err != nil {
				return nil, false, err
			}
			results = append(results, rows...)
		}
		return trimEntityContentSearchPage(results, req.limit()), len(results) > req.limit(), nil
	}
	results, err := h.Content.SearchEntityContentAnyRepo(ctx, req.pattern(), probeLimit)
	return trimEntityContentSearchPage(results, req.limit()), len(results) > req.limit(), err
}

func writeContentSelectorError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if queryselector.IsNotFound(err) {
		status = http.StatusNotFound
	}
	querycontract.WriteError(w, status, err.Error())
}

func contentSearchResponse(results any, req contentSearchRequest, truncated bool) map[string]any {
	count := 0
	switch typed := results.(type) {
	case []querycontract.FileContent:
		count = len(typed)
	case []querycontract.EntityContent:
		count = len(typed)
	}
	return map[string]any{
		"results":        results,
		"matches":        results,
		"count":          count,
		"limit":          req.limit(),
		"offset":         req.offset(),
		"truncated":      truncated,
		"source_backend": "postgres_content_store",
	}
}

func trimFileContentSearchPage(results []querycontract.FileContent, limit int) []querycontract.FileContent {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}

func trimEntityContentSearchPage(results []querycontract.EntityContent, limit int) []querycontract.EntityContent {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}
