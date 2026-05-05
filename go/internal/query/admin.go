package query

import (
	"context"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// RecoveryService is the subset of recovery.Handler used by admin endpoints.
type RecoveryService interface {
	Refinalize(ctx context.Context, filter recovery.RefinalizeFilter) (recovery.RefinalizeResult, error)
	ReplayFailed(ctx context.Context, filter recovery.ReplayFilter) (recovery.ReplayResult, error)
}

// ReindexRequester is the subset of the reindex surface used by admin routes.
type ReindexRequester interface {
	RequestReindex(ctx context.Context, ingester string) error
}

// AdminDecisionRow is a query-layer view of a projection decision row,
// avoiding a direct dependency on the projector package.
type AdminDecisionRow struct {
	DecisionID        string
	DecisionType      string
	RepositoryID      string
	SourceRunID       string
	WorkItemID        string
	Subject           string
	ConfidenceScore   float64
	ConfidenceReason  string
	ProvenanceSummary map[string]any
	CreatedAt         time.Time
}

// AdminEvidenceRow is a query-layer view of a projection decision evidence row.
type AdminEvidenceRow struct {
	EvidenceID   string
	DecisionID   string
	FactID       *string
	EvidenceKind string
	Detail       map[string]any
	CreatedAt    time.Time
}

// AdminStore provides read and write access to admin-facing Postgres tables
// (fact_work_items, projection_decisions, fact_replay_events, fact_backfill_requests).
type AdminStore interface {
	ListWorkItems(ctx context.Context, f WorkItemFilter) ([]AdminWorkItem, error)
	DeadLetterWorkItems(ctx context.Context, f DeadLetterFilter) ([]AdminWorkItem, error)
	SkipRepositoryWorkItems(ctx context.Context, repoID string, note string) ([]AdminWorkItem, error)
	ReplayFailedWorkItems(ctx context.Context, f ReplayWorkItemFilter) ([]AdminWorkItem, error)
	RequestBackfill(ctx context.Context, input BackfillInput) (*AdminBackfillRequest, error)
	ListReplayEvents(ctx context.Context, f ReplayEventFilter) ([]AdminReplayEvent, error)
	ListDecisions(ctx context.Context, f DecisionQueryFilter) ([]AdminDecisionRow, error)
	ListEvidence(ctx context.Context, decisionID string) ([]AdminEvidenceRow, error)
}

// AdminWorkItem is an admin-friendly view of a fact_work_items row.
type AdminWorkItem struct {
	WorkItemID     string     `json:"work_item_id"`
	ScopeID        string     `json:"scope_id"`
	GenerationID   string     `json:"generation_id"`
	Stage          string     `json:"stage"`
	Domain         string     `json:"domain"`
	Status         string     `json:"status"`
	AttemptCount   int        `json:"attempt_count"`
	LeaseOwner     *string    `json:"lease_owner"`
	FailureClass   *string    `json:"failure_class"`
	FailureMessage *string    `json:"failure_message"`
	OperatorNote   *string    `json:"operator_note"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	VisibleAt      *time.Time `json:"visible_at"`
}

// AdminReplayEvent is an admin-friendly view of a fact_replay_events row.
type AdminReplayEvent struct {
	ReplayEventID string    `json:"replay_event_id"`
	WorkItemID    string    `json:"work_item_id"`
	ScopeID       string    `json:"scope_id"`
	GenerationID  string    `json:"generation_id"`
	FailureClass  *string   `json:"failure_class"`
	OperatorNote  *string   `json:"operator_note"`
	CreatedAt     time.Time `json:"created_at"`
}

// AdminBackfillRequest is an admin-friendly view of a fact_backfill_requests row.
type AdminBackfillRequest struct {
	BackfillRequestID string    `json:"backfill_request_id"`
	ScopeID           *string   `json:"scope_id"`
	GenerationID      *string   `json:"generation_id"`
	OperatorNote      *string   `json:"operator_note"`
	CreatedAt         time.Time `json:"created_at"`
}

// WorkItemFilter constrains admin work-item queries.
type WorkItemFilter struct {
	Statuses     []string
	ScopeID      string
	Stage        string
	FailureClass string
	Limit        int
}

// DeadLetterFilter constrains admin dead-letter operations.
type DeadLetterFilter struct {
	WorkItemIDs  []string
	ScopeID      string
	Stage        string
	FailureClass string
	OperatorNote string
	Limit        int
}

// ReplayWorkItemFilter constrains admin replay operations.
type ReplayWorkItemFilter struct {
	WorkItemIDs  []string
	ScopeID      string
	Stage        string
	FailureClass string
	OperatorNote string
	Limit        int
}

// BackfillInput captures the parameters for a backfill request.
type BackfillInput struct {
	ScopeID      string
	GenerationID string
	OperatorNote string
}

// ReplayEventFilter constrains replay-event audit queries.
type ReplayEventFilter struct {
	ScopeID      string
	WorkItemID   string
	FailureClass string
	Limit        int
}

// DecisionQueryFilter constrains projection-decision queries.
type DecisionQueryFilter struct {
	RepositoryID    string
	SourceRunID     string
	DecisionType    *string
	IncludeEvidence bool
	Limit           int
}

// AdminHandler provides HTTP endpoints for administrative operations
// including recovery, work-item inspection, and fact-queue management.
type AdminHandler struct {
	Recovery  RecoveryService
	Reindexer ReindexRequester
	Store     AdminStore
}

// Mount registers all admin routes on the given mux.
func (h *AdminHandler) Mount(mux *http.ServeMux) {
	// Core admin endpoints (from admin.py)
	mux.HandleFunc("POST /api/v0/admin/refinalize", h.refinalize)
	mux.HandleFunc("GET /api/v0/admin/shared-projection/tuning-report", h.tuningReport)
	mux.HandleFunc("POST /api/v0/admin/reindex", h.reindex)

	// Fact-inspection endpoints (from admin_facts.py)
	mux.HandleFunc("POST /api/v0/admin/work-items/query", h.listWorkItems)
	mux.HandleFunc("POST /api/v0/admin/decisions/query", h.listDecisions)
	mux.HandleFunc("POST /api/v0/admin/dead-letter", h.deadLetter)
	mux.HandleFunc("POST /api/v0/admin/skip", h.skip)
	mux.HandleFunc("POST /api/v0/admin/replay", h.replay)
	mux.HandleFunc("POST /api/v0/admin/backfill", h.backfill)
	mux.HandleFunc("POST /api/v0/admin/replay-events/query", h.listReplayEvents)
}

// refinalize re-enqueues projector work for the given scope IDs.
// POST /api/v0/admin/refinalize
func (h *AdminHandler) refinalize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScopeIDs []string `json:"scope_ids"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.ScopeIDs) == 0 {
		WriteError(w, http.StatusBadRequest, "scope_ids is required and must not be empty")
		return
	}
	if h.Recovery == nil {
		WriteError(w, http.StatusServiceUnavailable, "recovery handler not configured")
		return
	}

	result, err := h.Recovery.Refinalize(r.Context(), recovery.RefinalizeFilter{
		ScopeIDs: req.ScopeIDs,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"status":    "accepted",
		"enqueued":  result.Enqueued,
		"scope_ids": result.ScopeIDs,
	})
}

// tuningReport returns shared-projection tuning information.
// GET /api/v0/admin/shared-projection/tuning-report
func (h *AdminHandler) tuningReport(w http.ResponseWriter, _ *http.Request) {
	// Shared-write tuning is managed by the projector pipeline internally.
	// This endpoint returns a static report indicating the Go-owned surface.
	WriteJSON(w, http.StatusOK, map[string]any{
		"status": "not_applicable",
		"detail": "Shared-projection tuning is managed internally by the Go projector pipeline.",
	})
}

// reindex accepts a reindex request and acknowledges it.
// POST /api/v0/admin/reindex
func (h *AdminHandler) reindex(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ingester string `json:"ingester"`
		Scope    string `json:"scope"`
		Force    bool   `json:"force"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Ingester == "" {
		req.Ingester = "repository"
	}
	if req.Scope == "" {
		req.Scope = "workspace"
	}
	if h.Reindexer == nil {
		WriteError(w, http.StatusServiceUnavailable, "reindex handler not configured")
		return
	}
	if err := h.Reindexer.RequestReindex(r.Context(), req.Ingester); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusAccepted, map[string]any{
		"status":   "accepted",
		"ingester": req.Ingester,
		"scope":    req.Scope,
		"force":    req.Force,
		"detail":   "Reindex request accepted. The ingester will process this on its next cycle.",
	})
}
