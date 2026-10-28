// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

const dockerfileInstallSearchLimit = 50

type dockerfileInstallEntitySearcher interface {
	SearchDockerfileInstallEntities(context.Context, string, []string, string, string, int, string, int) ([]querycontract.EntityContent, error)
}

type dockerfileInstallCursor struct {
	Basis        string `json:"basis"`
	RepoID       string `json:"repo_id"`
	RelativePath string `json:"relative_path"`
	StartLine    int    `json:"start_line"`
	EntityID     string `json:"entity_id"`
}

type dockerfileInstallSearchRequest struct {
	Package string                   `json:"package"`
	RepoID  string                   `json:"repo_id"`
	RepoIDs []string                 `json:"repo_ids"`
	Limit   int                      `json:"limit"`
	Cursor  *dockerfileInstallCursor `json:"cursor,omitempty"`
}

// searchDockerfileInstalls pages persisted install entities by their full,
// unique sort key. Authorization is resolved before the bounded entity query.
func (h *ContentHandler) searchDockerfileInstalls(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "code_search.content_search") {
		querycontract.WriteContractError(w, r, http.StatusNotImplemented, "content search is unavailable", querycontract.ErrorCodeUnsupportedCapability, "code_search.content_search", h.profile(), querycontract.RequiredProfile("code_search.content_search"))
		return
	}
	var req dockerfileInstallSearchRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Package)
	if !validDockerfileInstallName(name) {
		querycontract.WriteError(w, http.StatusBadRequest, "package must be an exact package name of at most 256 bytes")
		return
	}
	if req.Limit < 0 || req.Limit > dockerfileInstallSearchLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be between 0 and 50")
		return
	}
	if req.Limit == 0 {
		req.Limit = 10
	}
	if req.RepoID != "" && len(req.RepoIDs) > 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "use repo_id or repo_ids")
		return
	}
	cursor := dockerfileInstallCursor{}
	if req.Cursor != nil {
		cursor = *req.Cursor
		if cursor.RepoID == "" || cursor.RelativePath == "" || cursor.EntityID == "" || cursor.StartLine <= 0 || cursor.Basis == "" {
			querycontract.WriteError(w, http.StatusBadRequest, "cursor requires basis, repo_id, relative_path, start_line and entity_id")
			return
		}
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeDockerfileInstallSearch(w, r, name, nil, false, nil, req.Limit)
		return
	}
	normalized, err := h.normalizeContentSearchRequest(r.Context(), contentSearchRequest{RepoID: req.RepoID, RepoIDs: req.RepoIDs})
	if err != nil {
		writeContentSelectorError(w, r, err, "code_search.content_search")
		return
	}
	repoIDs := normalized.explicitRepoIDs()
	if len(repoIDs) == 0 && !access.Scoped() {
		repoIDs = nil
	}
	if normalized.repoID() != "" {
		repoIDs = []string{normalized.repoID()}
	}
	if access.Scoped() && len(repoIDs) == 0 {
		h.writeDockerfileInstallSearch(w, r, name, nil, false, nil, req.Limit)
		return
	}
	basis := dockerfileInstallCursorBasis(name, repoIDs)
	if req.Cursor != nil && cursor.Basis != basis {
		querycontract.WriteError(w, http.StatusBadRequest, "cursor does not match package and repository scope")
		return
	}
	searcher, ok := h.Content.(dockerfileInstallEntitySearcher)
	if !ok {
		querycontract.WriteError(w, http.StatusNotImplemented, "Dockerfile install search unavailable")
		return
	}
	entities, err := searcher.SearchDockerfileInstallEntities(r.Context(), name, repoIDs, cursor.RepoID, cursor.RelativePath, cursor.StartLine, cursor.EntityID, req.Limit+1)
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, "Dockerfile install search failed")
		return
	}
	truncated := len(entities) > req.Limit
	if truncated {
		entities = entities[:req.Limit]
	}
	rows := make([]map[string]any, 0, len(entities))
	for _, entity := range entities {
		row := map[string]any{
			"entity_id": entity.EntityID, "repo_id": entity.RepoID,
			"relative_path": entity.RelativePath, "line_number": entity.StartLine,
			"name": entity.EntityName, "manager": entity.Metadata["manager"],
			"category": entity.Metadata["category"], "stage": entity.Metadata["stage"],
		}
		if version, ok := entity.Metadata["version"]; ok {
			row["version"] = version
		}
		rows = append(rows, row)
	}
	var next any
	if truncated && len(entities) > 0 {
		last := entities[len(entities)-1]
		next = dockerfileInstallCursor{Basis: basis, RepoID: last.RepoID, RelativePath: last.RelativePath, StartLine: last.StartLine, EntityID: last.EntityID}
	}
	h.writeDockerfileInstallSearch(w, r, name, rows, truncated, next, req.Limit)
}

func (h *ContentHandler) writeDockerfileInstallSearch(w http.ResponseWriter, r *http.Request, name string, rows []map[string]any, truncated bool, cursor any, limit int) {
	if rows == nil {
		rows = []map[string]any{}
	}
	data := map[string]any{"package": name, "matches": rows, "limit": limit, "truncated": truncated}
	if cursor != nil {
		data["next_cursor"] = cursor
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, data, querycontract.BuildTruthEnvelope(h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex, "resolved from indexed Dockerfile install entities"))
}

func validDockerfileInstallName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for _, char := range name {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return false
		}
	}
	return true
}

func dockerfileInstallCursorBasis(name string, repoIDs []string) string {
	ids := slices.Clone(repoIDs)
	slices.Sort(ids)
	sum := sha256.Sum256([]byte(name + "\x00" + strings.Join(ids, "\x00")))
	return fmt.Sprintf("%x", sum)
}
