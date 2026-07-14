// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	inputInvalidFactListSchemaVersion = "eshu.admin.input_invalid_facts.v1"
	inputInvalidFactListMaxLimit      = 500
	inputInvalidFactListMaxTimeout    = 30 * time.Second
)

// AdminInputInvalidFactListHandler mounts only the bounded
// reducer_input_invalid_facts read surface (issue #4630), mirroring
// AdminDeadLetterListHandler's read-only mount for cmd/mcp-server (which has
// no full AdminHandler exposing mutations).
type AdminInputInvalidFactListHandler struct {
	Store       AdminStore
	Instruments *telemetry.Instruments
}

// Mount registers the input-invalid-facts list read without exposing admin
// mutations.
func (h *AdminInputInvalidFactListHandler) Mount(mux *http.ServeMux) {
	admin := &AdminHandler{Store: h.Store, Instruments: h.Instruments}
	mux.HandleFunc("POST /api/v0/admin/input-invalid-facts/query", admin.listInputInvalidFacts)
}

// listInputInvalidFacts returns a bounded, scoped page of durable
// reducer_input_invalid_facts rows for one scope generation (issue #4630).
// POST /api/v0/admin/input-invalid-facts/query
func (h *AdminHandler) listInputInvalidFacts(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		ScopeID      string `json:"scope_id"`
		GenerationID string `json:"generation_id"`
		Domain       string `json:"domain"`
		FactKind     string `json:"fact_kind"`
		Limit        int    `json:"limit"`
		TimeoutMS    int    `json:"timeout_ms"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.ScopeID) == "" || strings.TrimSpace(req.GenerationID) == "" {
		WriteError(w, http.StatusBadRequest, "scope_id and generation_id are required")
		return
	}
	if req.Limit <= 0 {
		WriteError(w, http.StatusBadRequest, "limit is required and must be between 1 and 500")
		return
	}
	if req.Limit > inputInvalidFactListMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit must be <= 500")
		return
	}
	if req.TimeoutMS <= 0 {
		WriteError(w, http.StatusBadRequest, "timeout_ms is required and must be between 1 and 30000")
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout > inputInvalidFactListMaxTimeout {
		WriteError(w, http.StatusBadRequest, "timeout_ms must be <= 30000")
		return
	}

	scopeID := strings.TrimSpace(req.ScopeID)
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		writeInputInvalidFactList(w, req.Limit, false, nil)
		return
	}

	// Authorization for a scoped token is delegated to the store query,
	// which joins ingestion_scopes and authorizes ScopeID via either a
	// direct scope grant or a repository grant (mirrors
	// ListDeadLetterWorkItems). An in-memory pre-check comparing scopeID
	// against the combined allowed-IDs map is NOT sufficient here: a
	// repository-scoped token grants the repository identifier, not the
	// raw ingestion scope_id, so that comparison would falsely reject a
	// request for a scope_id that legitimately belongs to an allowed
	// repository (codex review on PR #5252, issue #4630).
	filter := InputInvalidFactListFilter{
		ScopeID:              scopeID,
		GenerationID:         strings.TrimSpace(req.GenerationID),
		Domain:               strings.TrimSpace(req.Domain),
		FactKind:             strings.TrimSpace(req.FactKind),
		AllowedRepositoryIDs: access.grantedRepositoryIDs(),
		AllowedScopeIDs:      access.grantedScopeIDs(),
		Limit:                req.Limit + 1,
		Timeout:              timeout,
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	items, err := h.Store.ListReducerInputInvalidFacts(ctx, filter)
	h.recordInputInvalidFactsQuery(ctx, start, err)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			WriteError(w, http.StatusGatewayTimeout, "input-invalid-facts query timed out")
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list input invalid facts: %v", err))
		return
	}
	truncated := len(items) > req.Limit
	if truncated {
		items = items[:req.Limit]
	}
	writeInputInvalidFactList(w, req.Limit, truncated, items)
}

// recordInputInvalidFactsQuery records the query-duration histogram and, on
// failure, the reason-labeled error counter for the bounded
// reducer_input_invalid_facts read (issue #4630). A nil h.Instruments (the
// default for callers that have not wired telemetry) makes this a no-op.
func (h *AdminHandler) recordInputInvalidFactsQuery(ctx context.Context, start time.Time, err error) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.QueryInputInvalidFactsDuration != nil {
		h.Instruments.QueryInputInvalidFactsDuration.Record(ctx, time.Since(start).Seconds())
	}
	if err == nil || h.Instruments.QueryInputInvalidFactsErrors == nil {
		return
	}
	reason := "store_error"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "timeout"
	}
	h.Instruments.QueryInputInvalidFactsErrors.Add(ctx, 1, metric.WithAttributes(telemetry.AttrReason(reason)))
}

func writeInputInvalidFactList(w http.ResponseWriter, limit int, truncated bool, items []AdminReducerInputInvalidFact) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"schema_version": inputInvalidFactListSchemaVersion,
		"limit":          limit,
		"count":          len(items),
		"truncated":      truncated,
		"items":          nonNilInputInvalidFacts(items),
	})
}

func nonNilInputInvalidFacts(items []AdminReducerInputInvalidFact) []AdminReducerInputInvalidFact {
	if items == nil {
		return []AdminReducerInputInvalidFact{}
	}
	return items
}
