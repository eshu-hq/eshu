// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// decisionQueryFailedMessage and evidenceQueryFailedMessage are the fixed
// client-facing texts for a failed decision or evidence step. A store error
// quotes SQL, hosts, and connection settings, so err.Error() never reaches
// the body.
const (
	decisionQueryFailedMessage = "projection decision query failed"
	evidenceQueryFailedMessage = "projection decision evidence query failed"
)

// DecisionStore is the minimal port the decisions query reads through: one
// bounded decision list plus the per-decision evidence join. The full
// ReadStore satisfies it.
type DecisionStore interface {
	ListDecisions(ctx context.Context, f DecisionQueryFilter) ([]DecisionRow, error)
	ListEvidence(ctx context.Context, decisionID string) ([]EvidenceRow, error)
}

// ServeDecisionsQuery answers the projection-decision inspection read
// (POST /api/v0/admin/decisions/query): nil store 503s, bad JSON or a
// missing repository_id/source_run_id 400s, an absent limit defaults to 100
// with selectors trimmed, and success answers {count, decisions} with core
// field/RFC3339 parity, joining evidence per decision when requested. A
// store failure first offers onStoreError the fence verdicts (nil skips);
// then a client cancel answers 499, anything else the fixed 500.
func ServeDecisionsQuery(w http.ResponseWriter, r *http.Request, store DecisionStore, onStoreError StoreErrorHandler) {
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		RepositoryID    string  `json:"repository_id"`
		SourceRunID     string  `json:"source_run_id"`
		DecisionType    *string `json:"decision_type"`
		IncludeEvidence bool    `json:"include_evidence"`
		Limit           int     `json:"limit"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.RepositoryID) == "" || strings.TrimSpace(req.SourceRunID) == "" {
		writeError(w, http.StatusBadRequest, "repository_id and source_run_id are required")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = workItemsDefaultLimit
	}

	decisions, err := store.ListDecisions(r.Context(), DecisionQueryFilter{
		RepositoryID:    strings.TrimSpace(req.RepositoryID),
		SourceRunID:     strings.TrimSpace(req.SourceRunID),
		DecisionType:    req.DecisionType,
		IncludeEvidence: req.IncludeEvidence,
		Limit:           limit,
	})
	if err != nil {
		if onStoreError != nil && onStoreError(w, r, err) {
			return
		}
		if clientCanceled(r.Context(), err) {
			writeError(w, clientClosedStatus, decisionQueryFailedMessage)
			return
		}
		writeError(w, http.StatusInternalServerError, decisionQueryFailedMessage)
		return
	}

	evidenceByDecision := map[string][]map[string]any{}
	if req.IncludeEvidence {
		for _, d := range decisions {
			rows, evErr := store.ListEvidence(r.Context(), d.DecisionID)
			if evErr != nil {
				if onStoreError != nil && onStoreError(w, r, evErr) {
					return
				}
				if clientCanceled(r.Context(), evErr) {
					writeError(w, clientClosedStatus, evidenceQueryFailedMessage)
					return
				}
				writeError(w, http.StatusInternalServerError, evidenceQueryFailedMessage)
				return
			}
			evidenceByDecision[d.DecisionID] = EvidenceRowsToSlice(rows)
		}
	}

	decisionSlice := make([]map[string]any, 0, len(decisions))
	for _, d := range decisions {
		entry := DecisionRowToMap(d)
		if evidence, ok := evidenceByDecision[d.DecisionID]; ok {
			entry["evidence"] = evidence
		}
		decisionSlice = append(decisionSlice, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":     len(decisions),
		"decisions": decisionSlice,
	})
}

// DecisionRowToMap converts a DecisionRow to a JSON-friendly map with core
// parity: fixed field names and an RFC3339 timestamp.
func DecisionRowToMap(d DecisionRow) map[string]any {
	return map[string]any{
		"decision_id":        d.DecisionID,
		"decision_type":      d.DecisionType,
		"repository_id":      d.RepositoryID,
		"source_run_id":      d.SourceRunID,
		"work_item_id":       d.WorkItemID,
		"subject":            d.Subject,
		"confidence_score":   d.ConfidenceScore,
		"confidence_reason":  d.ConfidenceReason,
		"provenance_summary": d.ProvenanceSummary,
		"created_at":         d.CreatedAt.Format(time.RFC3339),
	}
}

// EvidenceRowsToSlice converts a slice of EvidenceRow to JSON-friendly maps
// with core parity: fixed field names, an RFC3339 timestamp, and a nil
// FactID omitted.
func EvidenceRowsToSlice(rows []EvidenceRow) []map[string]any {
	items := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		item := map[string]any{
			"evidence_id":   e.EvidenceID,
			"decision_id":   e.DecisionID,
			"evidence_kind": e.EvidenceKind,
			"detail":        e.Detail,
			"created_at":    e.CreatedAt.Format(time.RFC3339),
		}
		if e.FactID != nil {
			item["fact_id"] = *e.FactID
		}
		items = append(items, item)
	}
	return items
}
