// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// inputInvalidFactsQueryFailedMessage is the fixed client-facing text for a
// failed input-invalid-facts query step. A store error quotes SQL, hosts,
// and connection settings, so err.Error() never reaches the body.
const inputInvalidFactsQueryFailedMessage = "input-invalid-facts query failed"

// InputInvalidStore is the minimal port the input-invalid-facts query reads
// through: one bounded reducer_input_invalid_facts list. The full ReadStore
// satisfies it.
type InputInvalidStore interface {
	ListReducerInputInvalidFacts(ctx context.Context, f InputInvalidFactListFilter) ([]InputInvalidFact, error)
}

// ServeInputInvalidFactsQuery answers the reducer_input_invalid_facts
// inspection read (POST /api/v0/admin/input-invalid-facts/query): nil store
// 503s, bad JSON or a missing scope_id/generation_id 400s, an absent limit
// defaults to 100 with selectors trimmed, and success answers {count,
// items} with core field/RFC3339 parity. A store failure first offers
// onStoreError the fence verdicts (nil skips); then a client cancel answers
// 499, anything else the fixed 500.
func ServeInputInvalidFactsQuery(w http.ResponseWriter, r *http.Request, store InputInvalidStore, onStoreError StoreErrorHandler) {
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		ScopeID      string `json:"scope_id"`
		GenerationID string `json:"generation_id"`
		Domain       string `json:"domain"`
		FactKind     string `json:"fact_kind"`
		Limit        int    `json:"limit"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.ScopeID) == "" || strings.TrimSpace(req.GenerationID) == "" {
		writeError(w, http.StatusBadRequest, "scope_id and generation_id are required")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = workItemsDefaultLimit
	}

	items, err := store.ListReducerInputInvalidFacts(r.Context(), InputInvalidFactListFilter{
		ScopeID:      strings.TrimSpace(req.ScopeID),
		GenerationID: strings.TrimSpace(req.GenerationID),
		Domain:       strings.TrimSpace(req.Domain),
		FactKind:     strings.TrimSpace(req.FactKind),
		Limit:        limit,
	})
	if err != nil {
		if onStoreError != nil && onStoreError(w, r, err) {
			return
		}
		if clientCanceled(r.Context(), err) {
			writeError(w, clientClosedStatus, inputInvalidFactsQueryFailedMessage)
			return
		}
		writeError(w, http.StatusInternalServerError, inputInvalidFactsQueryFailedMessage)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"items": InputInvalidFactsToSlice(items),
	})
}

// InputInvalidFactsToSlice converts a slice of InputInvalidFact to
// JSON-friendly maps with core parity: the struct JSON-tag field names and
// an RFC3339 decided_at.
func InputInvalidFactsToSlice(items []InputInvalidFact) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"fact_id":       item.FactID,
			"fact_kind":     item.FactKind,
			"missing_field": item.MissingField,
			"failure_class": item.FailureClass,
			"domain":        item.Domain,
			"scope_id":      item.ScopeID,
			"generation_id": item.GenerationID,
			"decided_at":    item.DecidedAt.Format(time.RFC3339),
		})
	}
	return result
}
