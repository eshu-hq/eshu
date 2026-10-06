// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/runtime"
)

// reindexScopeWorkspace is the only reindex scope: every repository the git
// ingesters own.
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
type reindexRequest struct {
	Ingester string `json:"ingester"`
	Scope    string `json:"scope"`
	Force    *bool  `json:"force"`
}

// reindex records a fleet-wide reindex watermark and returns it.
// POST /api/v0/admin/reindex
func (h *Handler) reindex(w http.ResponseWriter, r *http.Request) {
	req, err := decodeReindexRequest(r)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
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
// defaults. Unknown fields are rejected so the retired workspace
// {scope, path, action} body fails instead of being accepted and ignored.
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
	if req.Ingester == "" {
		req.Ingester = runtime.ReindexIngesterRepository
	}
	if req.Ingester != runtime.ReindexIngesterRepository {
		return req, fmt.Errorf("ingester must be %q: only the git repository ingesters honor reindex requests", runtime.ReindexIngesterRepository)
	}
	if req.Scope == "" {
		req.Scope = reindexScopeWorkspace
	}
	if req.Scope != reindexScopeWorkspace {
		return req, fmt.Errorf("scope must be %q: this route forces a fleet-wide re-parse; per-repository reindex is not supported", reindexScopeWorkspace)
	}
	if req.Force != nil && !*req.Force {
		return req, errors.New("force must be true or omitted: a reindex always forces a full re-parse")
	}
	return req, nil
}
