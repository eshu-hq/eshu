// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// poisonedLinksQueryFailedMessage is the fixed client-facing text for a
// failed changed-since poisoned-links query step. A store error quotes SQL,
// hosts, and connection settings, so err.Error() never reaches the body.
const poisonedLinksQueryFailedMessage = "changed-since poisoned-links query failed"

// PoisonedLinkStore is the minimal port the poisoned-links query reads
// through: one bounded changed-since poisoned/retrying link list. The full
// ReadStore satisfies it.
type PoisonedLinkStore interface {
	ListChangedSincePoisonedLinks(ctx context.Context, f ChangedSincePoisonedLinkFilter) ([]ChangedSincePoisonedLink, error)
}

// ServePoisonedLinksQuery answers the changed-since poisoned/retrying link
// inspection read (POST /api/v0/admin/changed-since/poisoned-links/query):
// nil store 503s, bad JSON or a status outside poisoned|retrying 400s, an
// absent limit defaults to 100 with selectors trimmed, and success answers
// {count, items} with core field/RFC3339/pointer-omission parity. A store
// failure first offers onStoreError the fence verdicts (nil skips); then a
// client cancel answers 499, anything else the fixed 500.
func ServePoisonedLinksQuery(w http.ResponseWriter, r *http.Request, store PoisonedLinkStore, onStoreError StoreErrorHandler) {
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		Status  string `json:"status"`
		ScopeID string `json:"scope_id"`
		Cursor  string `json:"cursor"`
		Limit   int    `json:"limit"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && status != "poisoned" && status != "retrying" {
		writeError(w, http.StatusBadRequest, "status must be poisoned or retrying")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = workItemsDefaultLimit
	}

	items, err := store.ListChangedSincePoisonedLinks(r.Context(), ChangedSincePoisonedLinkFilter{
		Status:  status,
		ScopeID: strings.TrimSpace(req.ScopeID),
		Cursor:  strings.TrimSpace(req.Cursor),
		Limit:   limit,
	})
	if err != nil {
		if onStoreError != nil && onStoreError(w, r, err) {
			return
		}
		if clientCanceled(r.Context(), err) {
			writeError(w, clientClosedStatus, poisonedLinksQueryFailedMessage)
			return
		}
		writeError(w, http.StatusInternalServerError, poisonedLinksQueryFailedMessage)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"items": PoisonedLinksToSlice(items),
	})
}

// PoisonedLinksToSlice converts a slice of ChangedSincePoisonedLink to
// JSON-friendly maps with core parity: the struct JSON-tag field names,
// RFC3339 timestamps, and nil pointers omitted.
func PoisonedLinksToSlice(items []ChangedSincePoisonedLink) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"scope_id":       item.ScopeID,
			"status":         item.Status,
			"activation_seq": item.ActivationSeq,
			"attempt_count":  item.AttemptCount,
			"updated_at":     item.UpdatedAt.Format(time.RFC3339),
		}
		if item.LastFailureClass != nil {
			entry["last_failure_class"] = *item.LastFailureClass
		}
		if item.NextAttemptAt != nil {
			entry["next_attempt_at"] = item.NextAttemptAt.Format(time.RFC3339)
		}
		if item.PoisonedAt != nil {
			entry["poisoned_at"] = item.PoisonedAt.Format(time.RFC3339)
		}
		result = append(result, entry)
	}
	return result
}
