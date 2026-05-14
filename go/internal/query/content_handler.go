package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

const (
	contentSearchDefaultLimit = 50
	contentSearchMaxLimit     = 200
)

// ContentHandler serves HTTP endpoints for reading file and entity content
// from the Postgres content store.
type ContentHandler struct {
	Content ContentStore
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
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.RepoID == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if req.RelativePath == "" {
		WriteError(w, http.StatusBadRequest, "relative_path is required")
		return
	}
	resolvedRepoID, err := h.resolveRepositorySelector(r.Context(), req.RepoID)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.RepoID = resolvedRepoID

	fc, err := h.Content.GetFileContent(r.Context(), req.RepoID, req.RelativePath)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fc == nil {
		WriteError(w, http.StatusNotFound, "file not found")
		return
	}

	WriteJSON(w, http.StatusOK, fc)
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
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.RepoID == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if req.RelativePath == "" {
		WriteError(w, http.StatusBadRequest, "relative_path is required")
		return
	}
	resolvedRepoID, err := h.resolveRepositorySelector(r.Context(), req.RepoID)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.RepoID = resolvedRepoID

	fc, err := h.Content.GetFileLines(r.Context(), req.RepoID, req.RelativePath, req.StartLine, req.EndLine)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fc == nil {
		WriteError(w, http.StatusNotFound, "file not found")
		return
	}

	WriteJSON(w, http.StatusOK, fc)
}

// readEntity reads entity content by entity_id.
// POST /api/v0/content/entities/read
// Body: {"entity_id": "..."}
func (h *ContentHandler) readEntity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EntityID string `json:"entity_id"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.EntityID == "" {
		WriteError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	ec, err := h.Content.GetEntityContent(r.Context(), req.EntityID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ec == nil {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	WriteJSON(w, http.StatusOK, ec)
}

// searchFiles searches file content by pattern.
// POST /api/v0/content/files/search
// Body: {"repo_id": "...", "query": "...", "limit": 50}
func (h *ContentHandler) searchFiles(w http.ResponseWriter, r *http.Request) {
	req, err := readContentSearchRequest(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req, err = h.normalizeContentSearchRequest(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	results, truncated, err := h.searchFilesByScope(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, contentSearchResponse(results, req, truncated))
}

// searchEntities searches entity source cache by pattern.
// POST /api/v0/content/entities/search
// Body: {"repo_id": "...", "query": "...", "limit": 50}
func (h *ContentHandler) searchEntities(w http.ResponseWriter, r *http.Request) {
	req, err := readContentSearchRequest(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req, err = h.normalizeContentSearchRequest(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	results, truncated, err := h.searchEntitiesByScope(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, contentSearchResponse(results, req, truncated))
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
	if err := ReadJSON(r, &req); err != nil {
		return contentSearchRequest{}, err
	}
	return req, nil
}

func (req contentSearchRequest) validate() error {
	if req.pattern() == "" {
		return errors.New("query is required")
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
		return contentSearchDefaultLimit
	}
	if req.Limit > contentSearchMaxLimit {
		return contentSearchMaxLimit
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

func (h *ContentHandler) resolveRepositorySelector(ctx context.Context, selector string) (string, error) {
	return resolveRepositorySelectorExact(ctx, nil, h.Content, selector)
}

func (h *ContentHandler) normalizeContentSearchRequest(ctx context.Context, req contentSearchRequest) (contentSearchRequest, error) {
	if req.RepoID != "" {
		repoID, err := h.resolveRepositorySelector(ctx, req.RepoID)
		if err != nil {
			return contentSearchRequest{}, err
		}
		req.RepoID = repoID
		req.RepoIDs = nil
		return req, nil
	}
	if len(req.RepoIDs) == 0 {
		return req, nil
	}
	resolved := make([]string, 0, len(req.RepoIDs))
	seen := make(map[string]struct{}, len(req.RepoIDs))
	for _, selector := range req.RepoIDs {
		repoID, err := h.resolveRepositorySelector(ctx, selector)
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

func (h *ContentHandler) searchFilesByScope(ctx context.Context, req contentSearchRequest) ([]FileContent, bool, error) {
	if searcher, ok := h.Content.(pagedContentSearcher); ok {
		results, err := searcher.searchFiles(ctx, req)
		if err != nil {
			return nil, false, err
		}
		return trimFileContentSearchPage(results, req.limit()), len(results) > req.limit(), nil
	}
	if req.offset() > 0 {
		return nil, false, fmt.Errorf("content store does not support paged file search")
	}
	probeLimit := req.limit() + 1
	if repoID := req.repoID(); repoID != "" {
		results, err := h.Content.SearchFileContent(ctx, repoID, req.pattern(), probeLimit)
		return trimFileContentSearchPage(results, req.limit()), len(results) > req.limit(), err
	}
	if repoIDs := req.explicitRepoIDs(); len(repoIDs) > 0 {
		results := make([]FileContent, 0, probeLimit)
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

func (h *ContentHandler) searchEntitiesByScope(ctx context.Context, req contentSearchRequest) ([]EntityContent, bool, error) {
	if searcher, ok := h.Content.(pagedContentSearcher); ok {
		results, err := searcher.searchEntities(ctx, req)
		if err != nil {
			return nil, false, err
		}
		return trimEntityContentSearchPage(results, req.limit()), len(results) > req.limit(), nil
	}
	if req.offset() > 0 {
		return nil, false, fmt.Errorf("content store does not support paged entity search")
	}
	probeLimit := req.limit() + 1
	if repoID := req.repoID(); repoID != "" {
		results, err := h.Content.SearchEntityContent(ctx, repoID, req.pattern(), probeLimit)
		return trimEntityContentSearchPage(results, req.limit()), len(results) > req.limit(), err
	}
	if repoIDs := req.explicitRepoIDs(); len(repoIDs) > 0 {
		results := make([]EntityContent, 0, probeLimit)
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

func contentSearchResponse(results any, req contentSearchRequest, truncated bool) map[string]any {
	count := 0
	switch typed := results.(type) {
	case []FileContent:
		count = len(typed)
	case []EntityContent:
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

func trimFileContentSearchPage(results []FileContent, limit int) []FileContent {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}

func trimEntityContentSearchPage(results []EntityContent, limit int) []EntityContent {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}
