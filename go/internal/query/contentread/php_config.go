// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

const (
	phpConfigDefaultLimit = 50
	phpConfigMaxLimit     = 200
)

type phpConfigPageSearcher interface {
	SearchPHPConfigEntities(context.Context, []string, string, *querycontract.PHPConfigCursor, int) ([]querycontract.EntityContent, error)
}

type phpConfigSearchRequest struct {
	RepoID    string `json:"repo_id"`
	Directive string `json:"directive"`
	Mode      string `json:"mode"`
	Limit     int    `json:"limit"`
	Cursor    string `json:"cursor"`
}

// searchPHPConfig answers authored PHP declarations from indexed entities.
// The query cursor traverses the whole grant in stable entity order.
func (h *ContentHandler) searchPHPConfig(w http.ResponseWriter, r *http.Request) {
	var req phpConfigSearchRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if (req.Directive == "") == (req.Mode == "") {
		querycontract.WriteError(w, http.StatusBadRequest, "provide exactly one of directive or mode")
		return
	}
	if req.Limit == 0 {
		req.Limit = phpConfigDefaultLimit
	}
	if req.Limit < 1 || req.Limit > phpConfigMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be between 1 and 200")
		return
	}
	name := ""
	if req.Directive != "" {
		if !phpConfigNameAllowed(req.Directive) {
			querycontract.WriteError(w, http.StatusBadRequest, "invalid directive")
			return
		}
		name = "php_directive:" + strings.ToLower(req.Directive)
	} else {
		if req.Mode != "mod_php" && req.Mode != "php-fpm" {
			querycontract.WriteError(w, http.StatusBadRequest, "mode must be mod_php or php-fpm")
			return
		}
		name = "php_server_mode:" + req.Mode
	}
	searcher, ok := h.Content.(phpConfigPageSearcher)
	if !ok {
		querycontract.WriteError(w, http.StatusNotImplemented, "PHP configuration search unavailable")
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context()).WithCanonicalScopeRepositories()
	if access.Empty() {
		h.writePHPConfigSearch(w, r, []querycontract.EntityContent{}, req.Limit, false, name, req.RepoID)
		return
	}
	if req.RepoID != "" {
		repoID, err := h.resolveRepositorySelectorForAccess(r.Context(), req.RepoID, access)
		if err != nil {
			writeContentSelectorError(w, r, err, "code_search.content_search")
			return
		}
		req.RepoID = repoID
	}
	cursor, err := decodePHPConfigCursor(req.Cursor, name, req.RepoID)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var repoIDs []string
	if req.RepoID != "" {
		repoIDs = []string{req.RepoID}
	} else if access.Scoped() {
		repoIDs = access.RepositorySearchIDs()
	}
	results, err := searcher.SearchPHPConfigEntities(r.Context(), repoIDs, name, cursor, req.Limit+1)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "code_search.content_search") {
			return
		}
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, contentEntitySearchFailedMessage)
		return
	}
	truncated := len(results) > req.Limit
	if truncated {
		results = results[:req.Limit]
	}
	for i := range results {
		results[i].SourceCache = ""
	}
	h.writePHPConfigSearch(w, r, results, req.Limit, truncated, name, req.RepoID)
}

func (h *ContentHandler) writePHPConfigSearch(w http.ResponseWriter, r *http.Request, results []querycontract.EntityContent, limit int, truncated bool, name, repoID string) {
	response := map[string]any{"results": results, "count": len(results), "limit": limit, "truncated": truncated}
	if truncated {
		last := results[len(results)-1]
		position := querycontract.PHPConfigCursor{Name: name, FilterRepoID: repoID, RepoID: last.RepoID, RelativePath: last.RelativePath, StartLine: last.StartLine, EntityID: last.EntityID}
		encoded, _ := json.Marshal(position)
		response["next_cursor"] = base64.RawURLEncoding.EncodeToString(encoded)
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, response, querycontract.BuildTruthEnvelope(h.profile(), "code_search.content_search", querycontract.TruthBasisContentIndex, "resolved from guarded PHP configuration entities"))
}

func decodePHPConfigCursor(encoded, name, repoID string) (*querycontract.PHPConfigCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	if len(encoded) > 2048 {
		return nil, &phpConfigInputError{"cursor too long"}
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, &phpConfigInputError{"invalid cursor"}
	}
	var cursor querycontract.PHPConfigCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.RepoID == "" || cursor.RelativePath == "" || cursor.EntityID == "" || cursor.StartLine < 0 {
		return nil, &phpConfigInputError{"invalid cursor"}
	}
	if cursor.Name != name || cursor.FilterRepoID != repoID {
		return nil, &phpConfigInputError{"cursor does not match query"}
	}
	if repoID != "" && cursor.RepoID != repoID {
		return nil, &phpConfigInputError{"cursor outside repository"}
	}
	return &cursor, nil
}

type phpConfigInputError struct{ message string }

func (e *phpConfigInputError) Error() string { return e.message }

func phpConfigNameAllowed(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_', ch == '.', ch == '-':
		default:
			return false
		}
	}
	return true
}
