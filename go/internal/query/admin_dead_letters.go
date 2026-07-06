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
)

const (
	deadLetterListSchemaVersion = "eshu.admin.dead_letters.v1"
	deadLetterListMaxLimit      = 500
	deadLetterListMaxTimeout    = 30 * time.Second
)

// AdminDeadLetterListHandler mounts only the bounded dead-letter read surface.
type AdminDeadLetterListHandler struct {
	Store AdminStore
}

// Mount registers the dead-letter list read without exposing admin mutations.
func (h *AdminDeadLetterListHandler) Mount(mux *http.ServeMux) {
	admin := &AdminHandler{Store: h.Store}
	mux.HandleFunc("POST /api/v0/admin/dead-letters/query", admin.listDeadLetters)
}

// listDeadLetters returns a bounded, scoped page of durable dead-letter rows.
// POST /api/v0/admin/dead-letters/query
func (h *AdminHandler) listDeadLetters(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		FailureClass  string `json:"failure_class"`
		Domain        string `json:"domain"`
		ScopeID       string `json:"scope_id"`
		CollectorKind string `json:"collector_kind"`
		UpdatedAfter  string `json:"updated_after"`
		UpdatedBefore string `json:"updated_before"`
		Limit         int    `json:"limit"`
		TimeoutMS     int    `json:"timeout_ms"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Limit <= 0 {
		WriteError(w, http.StatusBadRequest, "limit is required and must be between 1 and 500")
		return
	}
	if req.Limit > deadLetterListMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit must be <= 500")
		return
	}
	if req.TimeoutMS <= 0 {
		WriteError(w, http.StatusBadRequest, "timeout_ms is required and must be between 1 and 30000")
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout > deadLetterListMaxTimeout {
		WriteError(w, http.StatusBadRequest, "timeout_ms must be <= 30000")
		return
	}

	updatedAfter, err := optionalRFC3339Time(req.UpdatedAfter, "updated_after")
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	updatedBefore, err := optionalRFC3339Time(req.UpdatedBefore, "updated_before")
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if updatedAfter != nil && updatedBefore != nil && !updatedAfter.Before(*updatedBefore) {
		WriteError(w, http.StatusBadRequest, "updated_after must be before updated_before")
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		writeDeadLetterList(w, req.Limit, false, nil)
		return
	}

	filter := DeadLetterListFilter{
		FailureClass:         strings.TrimSpace(req.FailureClass),
		Domain:               strings.TrimSpace(req.Domain),
		ScopeID:              strings.TrimSpace(req.ScopeID),
		CollectorKind:        strings.TrimSpace(req.CollectorKind),
		UpdatedAfter:         updatedAfter,
		UpdatedBefore:        updatedBefore,
		AllowedRepositoryIDs: access.grantedRepositoryIDs(),
		AllowedScopeIDs:      access.grantedScopeIDs(),
		Limit:                req.Limit + 1,
		Timeout:              timeout,
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	items, err := h.Store.ListDeadLetterWorkItems(ctx, filter)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			WriteError(w, http.StatusGatewayTimeout, "dead-letter query timed out")
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list dead letters: %v", err))
		return
	}
	truncated := len(items) > req.Limit
	if truncated {
		items = items[:req.Limit]
	}
	writeDeadLetterList(w, req.Limit, truncated, items)
}

func optionalRFC3339Time(value string, field string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339", field)
	}
	return &parsed, nil
}

func writeDeadLetterList(w http.ResponseWriter, limit int, truncated bool, items []AdminDeadLetterWorkItem) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"schema_version": deadLetterListSchemaVersion,
		"limit":          limit,
		"count":          len(items),
		"truncated":      truncated,
		"items":          deadLetterItemsToSlice(items),
	})
}

func deadLetterItemsToSlice(items []AdminDeadLetterWorkItem) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"work_item_id":   item.WorkItemID,
			"scope_id":       item.ScopeID,
			"generation_id":  item.GenerationID,
			"stage":          item.Stage,
			"domain":         item.Domain,
			"collector_kind": item.CollectorKind,
			"attempt_count":  item.AttemptCount,
			"created_at":     item.CreatedAt.Format(time.RFC3339),
			"updated_at":     item.UpdatedAt.Format(time.RFC3339),
		}
		if item.FailureClass != nil {
			entry["failure_class"] = *item.FailureClass
		}
		if item.VisibleAt != nil {
			entry["visible_at"] = item.VisibleAt.Format(time.RFC3339)
		}
		result = append(result, entry)
	}
	return result
}
