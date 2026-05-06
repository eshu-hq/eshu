package query

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// listWorkItems queries fact work items with optional filters.
// POST /api/v0/admin/work-items/query
func (h *AdminHandler) listWorkItems(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		Statuses     []string `json:"statuses"`
		ScopeID      string   `json:"scope_id"`
		Stage        string   `json:"stage"`
		FailureClass string   `json:"failure_class"`
		Limit        int      `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	items, err := h.Store.ListWorkItems(r.Context(), WorkItemFilter{
		Statuses:     req.Statuses,
		ScopeID:      strings.TrimSpace(req.ScopeID),
		Stage:        strings.TrimSpace(req.Stage),
		FailureClass: strings.TrimSpace(req.FailureClass),
		Limit:        limit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list work items: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"items": workItemsToSlice(items),
	})
}

// listDecisions queries projection decisions for a repository/run pair.
// POST /api/v0/admin/decisions/query
func (h *AdminHandler) listDecisions(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		RepositoryID    string  `json:"repository_id"`
		SourceRunID     string  `json:"source_run_id"`
		DecisionType    *string `json:"decision_type"`
		IncludeEvidence bool    `json:"include_evidence"`
		Limit           int     `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if strings.TrimSpace(req.RepositoryID) == "" || strings.TrimSpace(req.SourceRunID) == "" {
		WriteError(w, http.StatusBadRequest, "repository_id and source_run_id are required")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	decisions, err := h.Store.ListDecisions(r.Context(), DecisionQueryFilter{
		RepositoryID:    strings.TrimSpace(req.RepositoryID),
		SourceRunID:     strings.TrimSpace(req.SourceRunID),
		DecisionType:    req.DecisionType,
		IncludeEvidence: req.IncludeEvidence,
		Limit:           limit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list decisions: %v", err))
		return
	}

	// Build evidence lookup if requested.
	evidenceByDecision := map[string][]map[string]any{}
	if req.IncludeEvidence {
		for _, d := range decisions {
			rows, evErr := h.Store.ListEvidence(r.Context(), d.DecisionID)
			if evErr != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list evidence: %v", evErr))
				return
			}
			evidenceByDecision[d.DecisionID] = evidenceRowsToSlice(rows)
		}
	}

	decisionSlice := make([]map[string]any, 0, len(decisions))
	for _, d := range decisions {
		entry := decisionRowToMap(d)
		if evidence, ok := evidenceByDecision[d.DecisionID]; ok {
			entry["evidence"] = evidence
		}
		decisionSlice = append(decisionSlice, entry)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"count":     len(decisions),
		"decisions": decisionSlice,
	})
}

// deadLetter moves selected work items into durable dead-letter state.
// POST /api/v0/admin/dead-letter
func (h *AdminHandler) deadLetter(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		WorkItemIDs  []string `json:"work_item_ids"`
		ScopeID      string   `json:"scope_id"`
		Stage        string   `json:"stage"`
		FailureClass string   `json:"failure_class"`
		OperatorNote string   `json:"operator_note"`
		Limit        int      `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.WorkItemIDs) == 0 &&
		strings.TrimSpace(req.ScopeID) == "" &&
		strings.TrimSpace(req.Stage) == "" {
		WriteError(w, http.StatusBadRequest,
			"at least one selector is required: work_item_ids, scope_id, or stage")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	items, err := h.Store.DeadLetterWorkItems(r.Context(), DeadLetterFilter{
		WorkItemIDs:  req.WorkItemIDs,
		ScopeID:      strings.TrimSpace(req.ScopeID),
		Stage:        strings.TrimSpace(req.Stage),
		FailureClass: strings.TrimSpace(req.FailureClass),
		OperatorNote: strings.TrimSpace(req.OperatorNote),
		Limit:        limit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("dead-letter: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"items": workItemsToSlice(items),
	})
}

// skip marks one repository's actionable work items as intentionally skipped.
// POST /api/v0/admin/skip
func (h *AdminHandler) skip(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		RepositoryID string `json:"repository_id"`
		OperatorNote string `json:"operator_note"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	repoID := strings.TrimSpace(req.RepositoryID)
	if repoID == "" {
		WriteError(w, http.StatusBadRequest, "repository_id is required and must not be empty")
		return
	}

	items, err := h.Store.SkipRepositoryWorkItems(r.Context(), repoID, strings.TrimSpace(req.OperatorNote))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("skip: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"items": workItemsToSlice(items),
	})
}

// replay replays terminally failed fact-projection work items.
// POST /api/v0/admin/replay
func (h *AdminHandler) replay(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		WorkItemIDs  []string `json:"work_item_ids"`
		ScopeID      string   `json:"scope_id"`
		Stage        string   `json:"stage"`
		FailureClass string   `json:"failure_class"`
		OperatorNote string   `json:"operator_note"`
		Limit        int      `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.WorkItemIDs) == 0 &&
		strings.TrimSpace(req.ScopeID) == "" &&
		strings.TrimSpace(req.Stage) == "" &&
		strings.TrimSpace(req.FailureClass) == "" {
		WriteError(w, http.StatusBadRequest,
			"at least one selector is required: work_item_ids, scope_id, stage, or failure_class")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	items, err := h.Store.ReplayFailedWorkItems(r.Context(), ReplayWorkItemFilter{
		WorkItemIDs:  req.WorkItemIDs,
		ScopeID:      strings.TrimSpace(req.ScopeID),
		Stage:        strings.TrimSpace(req.Stage),
		FailureClass: strings.TrimSpace(req.FailureClass),
		OperatorNote: strings.TrimSpace(req.OperatorNote),
		Limit:        limit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("replay: %v", err))
		return
	}

	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.WorkItemID)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"status":         "replayed",
		"replayed_count": len(items),
		"work_item_ids":  ids,
		"replayed":       workItemsToSlice(items),
	})
}

// backfill creates one durable operator backfill request.
// POST /api/v0/admin/backfill
func (h *AdminHandler) backfill(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		ScopeID      string `json:"scope_id"`
		GenerationID string `json:"generation_id"`
		OperatorNote string `json:"operator_note"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if strings.TrimSpace(req.ScopeID) == "" && strings.TrimSpace(req.GenerationID) == "" {
		WriteError(w, http.StatusBadRequest,
			"at least one selector is required: scope_id or generation_id")
		return
	}

	row, err := h.Store.RequestBackfill(r.Context(), BackfillInput{
		ScopeID:      strings.TrimSpace(req.ScopeID),
		GenerationID: strings.TrimSpace(req.GenerationID),
		OperatorNote: strings.TrimSpace(req.OperatorNote),
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("backfill: %v", err))
		return
	}

	result := map[string]any{
		"backfill_request_id": row.BackfillRequestID,
		"created_at":          row.CreatedAt.Format(time.RFC3339),
	}
	if row.ScopeID != nil {
		result["scope_id"] = *row.ScopeID
	}
	if row.GenerationID != nil {
		result["generation_id"] = *row.GenerationID
	}
	if row.OperatorNote != nil {
		result["operator_note"] = *row.OperatorNote
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"status":           "accepted",
		"backfill_request": result,
	})
}

// listReplayEvents queries the durable replay-event audit log.
// POST /api/v0/admin/replay-events/query
func (h *AdminHandler) listReplayEvents(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		ScopeID      string `json:"scope_id"`
		WorkItemID   string `json:"work_item_id"`
		FailureClass string `json:"failure_class"`
		Limit        int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	events, err := h.Store.ListReplayEvents(r.Context(), ReplayEventFilter{
		ScopeID:      strings.TrimSpace(req.ScopeID),
		WorkItemID:   strings.TrimSpace(req.WorkItemID),
		FailureClass: strings.TrimSpace(req.FailureClass),
		Limit:        limit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list replay events: %v", err))
		return
	}

	eventSlice := make([]map[string]any, 0, len(events))
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
		eventSlice = append(eventSlice, entry)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"count":  len(events),
		"events": eventSlice,
	})
}

// decisionRowToMap converts an AdminDecisionRow to a JSON-friendly map.
func decisionRowToMap(d AdminDecisionRow) map[string]any {
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

// evidenceRowsToSlice converts a slice of AdminEvidenceRow to JSON-friendly maps.
func evidenceRowsToSlice(rows []AdminEvidenceRow) []map[string]any {
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

// workItemsToSlice converts a slice of AdminWorkItem to JSON-friendly maps.
func workItemsToSlice(items []AdminWorkItem) []map[string]any {
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
		if item.VisibleAt != nil {
			entry["visible_at"] = item.VisibleAt.Format(time.RFC3339)
		}
		result = append(result, entry)
	}
	return result
}
