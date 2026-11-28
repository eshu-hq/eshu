// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// deadLetterQueryFailedMessage is the fixed client-facing text for a failed
// dead-letter query step. A store error quotes SQL, hosts, and connection
// settings, so err.Error() never reaches the body.
const deadLetterQueryFailedMessage = "dead-letter query failed"

// DeadLetterStore is the minimal port the dead-letter query reads through:
// one bounded dead-letter list. The full ReadStore satisfies it.
type DeadLetterStore interface {
	ListDeadLetterWorkItems(ctx context.Context, f DeadLetterListFilter) ([]DeadLetterWorkItem, error)
}

// ServeDeadLettersQuery answers the dead-letter inspection read
// (POST /api/v0/admin/dead-letters/query): nil store 503s, bad JSON 400s, an
// absent limit defaults to 100 with selectors trimmed, optional
// updated_after/updated_before window bounds parse as RFC3339 (malformed or
// misordered windows 400 with the core-identical details) and pass through
// to the filter, and success answers {count, items} with core
// field/RFC3339/pointer-omission parity. A store failure first offers
// onStoreError the fence verdicts (nil skips); then a client cancel answers
// 499, anything else the fixed 500.
func ServeDeadLettersQuery(w http.ResponseWriter, r *http.Request, store DeadLetterStore, onStoreError StoreErrorHandler) {
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "admin store not configured")
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
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = workItemsDefaultLimit
	}

	updatedAfter, err := optionalDeadLetterRFC3339(req.UpdatedAfter, "updated_after")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updatedBefore, err := optionalDeadLetterRFC3339(req.UpdatedBefore, "updated_before")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if updatedAfter != nil && updatedBefore != nil && !updatedAfter.Before(*updatedBefore) {
		writeError(w, http.StatusBadRequest, "updated_after must be before updated_before")
		return
	}

	items, err := store.ListDeadLetterWorkItems(r.Context(), DeadLetterListFilter{
		FailureClass:  strings.TrimSpace(req.FailureClass),
		Domain:        strings.TrimSpace(req.Domain),
		ScopeID:       strings.TrimSpace(req.ScopeID),
		CollectorKind: strings.TrimSpace(req.CollectorKind),
		UpdatedAfter:  updatedAfter,
		UpdatedBefore: updatedBefore,
		Limit:         limit,
	})
	if err != nil {
		if onStoreError != nil && onStoreError(w, r, err) {
			return
		}
		if clientCanceled(r.Context(), err) {
			writeError(w, clientClosedStatus, deadLetterQueryFailedMessage)
			return
		}
		writeError(w, http.StatusInternalServerError, deadLetterQueryFailedMessage)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"items": DeadLetterItemsToSlice(items),
	})
}

// optionalDeadLetterRFC3339 parses an optional RFC3339 window bound, mirroring
// the core dead-letter query: blank stays nil, malformed answers
// "<field> must be RFC3339".
func optionalDeadLetterRFC3339(value string, field string) (*time.Time, error) {
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

// DeadLetterItemsToSlice converts a slice of DeadLetterWorkItem to
// JSON-friendly maps with core parity: fixed field names, RFC3339
// timestamps, and nil pointers omitted.
func DeadLetterItemsToSlice(items []DeadLetterWorkItem) []map[string]any {
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
