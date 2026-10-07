// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/runtime"
)

// reindexScopeWorkspace, the default scope, records the fleet watermark for
// every repository the git ingesters own.
const reindexScopeWorkspace = "workspace"

// reindexAcceptedDetail states the watermark contract the 202 response
// promises (#7620).
const reindexAcceptedDetail = "Reindex recorded. requested_at is the fleet reindex watermark: " +
	"on each sync cycle every git ingester shard forces a full re-parse of each repository whose " +
	"newest activated full generation was ingested before requested_at, within " +
	"ESHU_REPO_RECONCILE_MAX_PER_CYCLE per shard per cycle. The request is satisfied repository by " +
	"repository as those full generations activate; there is no separate completion status."

// reindexRequest is the POST /api/v0/admin/reindex body. Force is a pointer so
// an omitted field defaults to true while an explicit false is rejected.
// Repositories holds the selectors of a repository-scoped request.
type reindexRequest struct {
	Ingester     string   `json:"ingester"`
	Scope        string   `json:"scope"`
	Repositories []string `json:"repositories"`
	Force        *bool    `json:"force"`
}

// reindex records a fleet-wide reindex watermark, or per-repository
// watermarks for a repository-scoped request, and returns them.
// POST /api/v0/admin/reindex
func (h *Handler) reindex(w http.ResponseWriter, r *http.Request) {
	req, err := decodeReindexRequest(r)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Scope == reindexScopeRepository {
		h.reindexRepositories(w, r, req)
		return
	}
	if h.Reindexer == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "reindex handler not configured")
		return
	}
	requestedAt, err := h.Reindexer.RequestReindex(r.Context(), req.Ingester)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	querycontract.WriteJSON(w, http.StatusAccepted, map[string]any{
		"status":       "accepted",
		"ingester":     req.Ingester,
		"scope":        req.Scope,
		"force":        true,
		"requested_at": requestedAt.UTC().Format(time.RFC3339Nano),
		"detail":       reindexAcceptedDetail,
	})
}

// decodeReindexRequest decodes and validates the reindex body, filling the
// defaults. Unknown fields and trailing values are rejected so the retired
// workspace {scope, path, action} body fails instead of being accepted and
// ignored.
func decodeReindexRequest(r *http.Request) (reindexRequest, error) {
	var req reindexRequest
	if r.Body == nil {
		return req, errors.New("request body is required")
	}
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return req, errors.New("invalid JSON: unexpected trailing data after the request object")
	}
	if req.Ingester == "" {
		req.Ingester = runtime.ReindexIngesterRepository
	}
	if req.Ingester != runtime.ReindexIngesterRepository {
		return req, fmt.Errorf("ingester must be %q: only the git repository ingesters honor reindex requests", runtime.ReindexIngesterRepository)
	}
	if req.Scope == "" {
		req.Scope = reindexScopeWorkspace
	}
	switch req.Scope {
	case reindexScopeWorkspace:
		if req.Repositories != nil {
			return req, fmt.Errorf("repositories requires scope %q; scope %q reindexes every repository", reindexScopeRepository, reindexScopeWorkspace)
		}
	case reindexScopeRepository:
		if err := validateReindexRepositories(req.Repositories); err != nil {
			return req, err
		}
	default:
		return req, fmt.Errorf("scope must be %q or %q", reindexScopeWorkspace, reindexScopeRepository)
	}
	if req.Force != nil && !*req.Force {
		return req, errors.New("force must be true or omitted: a reindex always forces a full re-parse")
	}
	return req, nil
}

// validateReindexRepositories bounds a repository-scoped request to 1 to
// maxReindexRepositories selectors, none blank.
func validateReindexRepositories(selectors []string) error {
	if len(selectors) == 0 || len(selectors) > maxReindexRepositories {
		return fmt.Errorf("scope %q requires repositories with 1 to %d selectors, got %d", reindexScopeRepository, maxReindexRepositories, len(selectors))
	}
	for i, selector := range selectors {
		if strings.TrimSpace(selector) == "" {
			return fmt.Errorf("repositories[%d] is blank", i)
		}
	}
	return nil
}
