// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// replayEventQueryFailedMessage is the fixed client-facing text for a failed
// replay-event query step. A store error quotes SQL, hosts, and connection
// settings, so err.Error() never reaches the body.
const replayEventQueryFailedMessage = "replay-event query failed"

// ReplayEventStore is the minimal port the replay-events query reads through:
// one bounded replay-event list. The full ReadStore satisfies it.
type ReplayEventStore interface {
	ListReplayEvents(ctx context.Context, f ReplayEventFilter) ([]ReplayEvent, error)
}

// ServeReplayEventsQuery answers the replay-event audit read
// (POST /api/v0/admin/replay-events/query): nil store 503s, bad JSON 400s,
// an absent limit defaults to 100 with selectors trimmed, and success
// answers {count, events} with core field/RFC3339/pointer-omission parity.
// A store failure first offers onStoreError the fence verdicts (nil skips);
// then a client cancel answers 499, anything else the fixed 500.
func ServeReplayEventsQuery(w http.ResponseWriter, r *http.Request, store ReplayEventStore, onStoreError StoreErrorHandler) {
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		ScopeID      string `json:"scope_id"`
		WorkItemID   string `json:"work_item_id"`
		FailureClass string `json:"failure_class"`
		Limit        int    `json:"limit"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = workItemsDefaultLimit
	}

	events, err := store.ListReplayEvents(r.Context(), ReplayEventFilter{
		ScopeID:      strings.TrimSpace(req.ScopeID),
		WorkItemID:   strings.TrimSpace(req.WorkItemID),
		FailureClass: strings.TrimSpace(req.FailureClass),
		Limit:        limit,
	})
	if err != nil {
		if onStoreError != nil && onStoreError(w, r, err) {
			return
		}
		if clientCanceled(r.Context(), err) {
			writeError(w, clientClosedStatus, replayEventQueryFailedMessage)
			return
		}
		writeError(w, http.StatusInternalServerError, replayEventQueryFailedMessage)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":  len(events),
		"events": ReplayEventsToSlice(events),
	})
}

// ReplayEventsToSlice converts a slice of ReplayEvent to JSON-friendly maps
// with core parity: the entry keys the core audit read emits, RFC3339
// timestamps, and nil pointers omitted.
func ReplayEventsToSlice(events []ReplayEvent) []map[string]any {
	result := make([]map[string]any, 0, len(events))
	for _, e := range events {
		entry := map[string]any{
			"replay_event_id": e.ReplayEventID,
			"work_item_id":    e.WorkItemID,
			"scope_id":        e.ScopeID,
			"generation_id":   e.GenerationID,
			"created_at":      e.CreatedAt.Format(time.RFC3339),
		}
		if e.FailureClass != nil {
			entry["failure_class"] = *e.FailureClass
		}
		if e.OperatorNote != nil {
			entry["operator_note"] = *e.OperatorNote
		}
		result = append(result, entry)
	}
	return result
}
