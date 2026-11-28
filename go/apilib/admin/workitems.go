// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// workItemsDefaultLimit is the page size when the caller passes no positive
// limit, matching the core work-items query.
const workItemsDefaultLimit = 100

// workItemQueryFailedMessage is the fixed client-facing text for a failed
// work-items query step. A store error quotes SQL, hosts, and connection
// settings, so err.Error() never reaches the body.
const workItemQueryFailedMessage = "work-item query failed"

// clientClosedStatus is the nonstandard 499 status a query route answers
// when the caller canceled its own request before the server failed. The
// client is gone and never reads the body; the status exists for access
// logs, which tell it apart from a 500 server fault.
const clientClosedStatus = 499

// WorkItemStore is the minimal port the work-items query reads through: one
// bounded work-item list. The full ReadStore satisfies it.
type WorkItemStore interface {
	ListWorkItems(ctx context.Context, f WorkItemFilter) ([]WorkItem, error)
}

// StoreErrorHandler answers a store failure the handler cannot classify
// itself: the reader-verdict seam (stale or timed-out guarded reader, 503
// with Retry-After under a capability-matrix key). It reports whether it
// wrote the response; a false return (or a nil hook) falls through to the
// fixed 500/499 below.
type StoreErrorHandler func(w http.ResponseWriter, r *http.Request, err error) bool

// writeJSON writes one JSON response with HTML escaping off, matching the
// core querycontract envelope bytes.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

// writeError writes a plain JSON error response: the HTTP status text as
// error and the fixed caller message as detail.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": http.StatusText(status), "detail": message})
}

// readJSON decodes one request body and closes it.
func readJSON(r *http.Request, value any) error {
	if r.Body == nil {
		return errors.New("request body is required")
	}
	defer func() { _ = r.Body.Close() }()
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// clientCanceled reports whether err is the caller walking away from its own
// request: err wraps context.Canceled AND the request context itself is
// canceled. A context.Canceled from some inner context on a live request is
// a server fault, not a client cancel.
func clientCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled)
}

// ServeWorkItemsQuery answers the work-items inspection read
// (POST /api/v0/admin/work-items/query): nil store 503s, bad JSON 400s, an
// absent limit defaults to 100 with selectors trimmed, and success answers
// {count, items} with core field/RFC3339/pointer-omission parity. A store
// failure first offers onStoreError the fence verdicts (nil skips); then a
// client cancel answers 499, anything else the fixed 500.
func ServeWorkItemsQuery(w http.ResponseWriter, r *http.Request, store WorkItemStore, onStoreError StoreErrorHandler) {
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		Statuses     []string `json:"statuses"`
		ScopeID      string   `json:"scope_id"`
		Stage        string   `json:"stage"`
		FailureClass string   `json:"failure_class"`
		Limit        int      `json:"limit"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = workItemsDefaultLimit
	}

	items, err := store.ListWorkItems(r.Context(), WorkItemFilter{
		Statuses:     req.Statuses,
		ScopeID:      strings.TrimSpace(req.ScopeID),
		Stage:        strings.TrimSpace(req.Stage),
		FailureClass: strings.TrimSpace(req.FailureClass),
		Limit:        limit,
	})
	if err != nil {
		if onStoreError != nil && onStoreError(w, r, err) {
			return
		}
		if clientCanceled(r.Context(), err) {
			writeError(w, clientClosedStatus, workItemQueryFailedMessage)
			return
		}
		writeError(w, http.StatusInternalServerError, workItemQueryFailedMessage)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"items": WorkItemsToSlice(items),
	})
}

// WorkItemsToSlice converts a slice of WorkItem to JSON-friendly maps with
// core parity: fixed field names, RFC3339 timestamps, and nil pointers
// omitted.
func WorkItemsToSlice(items []WorkItem) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"work_item_id":  item.WorkItemID,
			"scope_id":      item.ScopeID,
			"generation_id": item.GenerationID,
			"stage":         item.Stage,
			"domain":        item.Domain,
			"status":        item.Status,
			"attempt_count": item.AttemptCount,
			"created_at":    item.CreatedAt.Format(time.RFC3339),
			"updated_at":    item.UpdatedAt.Format(time.RFC3339),
		}
		if item.LeaseOwner != nil {
			entry["lease_owner"] = *item.LeaseOwner
		}
		if item.FailureClass != nil {
			entry["failure_class"] = *item.FailureClass
		}
		if item.FailureMessage != nil {
			entry["failure_message"] = *item.FailureMessage
		}
		if item.OperatorNote != nil {
			entry["operator_note"] = *item.OperatorNote
		}
		if item.PriorFailure != nil {
			entry["prior_failure"] = item.PriorFailure
		}
		if item.VisibleAt != nil {
			entry["visible_at"] = item.VisibleAt.Format(time.RFC3339)
		}
		result = append(result, entry)
	}
	return result
}
