// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RecoveryService is the subset of recovery.Handler used by admin endpoints.
type RecoveryService interface {
	Refinalize(ctx context.Context, filter recovery.RefinalizeFilter) (recovery.RefinalizeResult, error)
	ReplayFailed(ctx context.Context, filter recovery.ReplayFilter) (recovery.ReplayResult, error)
}

// ReindexRequester records a fleet-wide reindex request for the admin reindex
// route and returns the stored reindex watermark (#7620).
type ReindexRequester interface {
	RequestReindex(ctx context.Context, ingester string) (time.Time, error)
}

// DecisionRow is a query-layer view of a projection decision row,
// avoiding a direct dependency on the projector package.
type DecisionRow struct {
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

// EvidenceRow is a query-layer view of a projection decision evidence row.
type EvidenceRow struct {
	EvidenceID   string
	DecisionID   string
	FactID       *string
	EvidenceKind string
	Detail       map[string]any
	CreatedAt    time.Time
}

// ReadStore provides bounded inspection without admin mutation methods.
type ReadStore interface {
	ListWorkItems(ctx context.Context, f WorkItemFilter) ([]WorkItem, error)
	ListDeadLetterWorkItems(ctx context.Context, f DeadLetterListFilter) ([]DeadLetterWorkItem, error)
	ListReducerInputInvalidFacts(ctx context.Context, f InputInvalidFactListFilter) ([]InputInvalidFact, error)
	ListChangedSincePoisonedLinks(ctx context.Context, f ChangedSincePoisonedLinkFilter) ([]ChangedSincePoisonedLink, error)
	ListReplayEvents(ctx context.Context, f ReplayEventFilter) ([]ReplayEvent, error)
	ListDecisions(ctx context.Context, f DecisionQueryFilter) ([]DecisionRow, error)
	ListEvidence(ctx context.Context, decisionID string) ([]EvidenceRow, error)
}

// Store combines inspection with authoritative admin mutations and replay state.
type Store interface {
	ReadStore
	DeadLetterWorkItems(ctx context.Context, f DeadLetterFilter) ([]WorkItem, error)
	SkipRepositoryWorkItems(ctx context.Context, repoID string, note string) ([]WorkItem, error)
	ReplayFailedWorkItems(ctx context.Context, f ReplayWorkItemFilter) ([]WorkItem, error)
	UnsafeReplayTargets(ctx context.Context, f UnsafeReplayTargetFilter) ([]UnsafeReplayTarget, error)
	SupersededReplayTargets(ctx context.Context, f UnsafeReplayTargetFilter) ([]SupersededReplayTarget, error)
	ClaimReplayIdempotency(ctx context.Context, key, fingerprint string, now time.Time) (ReplayIdempotencyClaim, error)
	CompleteReplayIdempotency(ctx context.Context, key string, count int, workItemIDs []string, now time.Time) error
	ReopenCompletedWork(ctx context.Context, f ReopenFilter) (ReopenResult, error)
	// ResolveReopenTarget resolves the scope selector to the canonical
	// scope id and active generation without mutating anything, so the
	// handler can refuse an unknown scope or a generation-less scope
	// before the idempotency claim instead of burning the key.
	ResolveReopenTarget(ctx context.Context, scopeID string) (scope string, generation string, err error)
	RequestBackfill(ctx context.Context, input BackfillInput) (*BackfillRequest, error)
}

// WorkItem is an admin-friendly view of a fact_work_items row.
type WorkItem struct {
	WorkItemID     string  `json:"work_item_id"`
	ScopeID        string  `json:"scope_id"`
	GenerationID   string  `json:"generation_id"`
	Stage          string  `json:"stage"`
	Domain         string  `json:"domain"`
	Status         string  `json:"status"`
	AttemptCount   int     `json:"attempt_count"`
	LeaseOwner     *string `json:"lease_owner"`
	FailureClass   *string `json:"failure_class"`
	FailureMessage *string `json:"failure_message"`
	OperatorNote   *string `json:"operator_note"`
	// PriorFailure is the failure the row carried before a supersede, a
	// stale-scope reclaim or an operator note rewrote its failure evidence
	// (#7320, #7388, #7385). Nil when
	// failure_details carries none. Its details text is not exposed.
	PriorFailure *PriorFailure `json:"prior_failure,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	VisibleAt    *time.Time    `json:"visible_at"`
}

// PriorFailure is the kept failure evidence of a work item: its status, class
// and message before the overwrite, and when the row was last updated then.
type PriorFailure struct {
	Status         string `json:"status,omitempty"`
	FailureClass   string `json:"failure_class,omitempty"`
	FailureMessage string `json:"failure_message,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

// DeadLetterWorkItem is a bounded operator-facing view of one durable
// fact_work_items dead-letter row.
type DeadLetterWorkItem struct {
	WorkItemID    string     `json:"work_item_id"`
	ScopeID       string     `json:"scope_id"`
	GenerationID  string     `json:"generation_id"`
	Stage         string     `json:"stage"`
	Domain        string     `json:"domain"`
	CollectorKind string     `json:"collector_kind"`
	AttemptCount  int        `json:"attempt_count"`
	FailureClass  *string    `json:"failure_class"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	VisibleAt     *time.Time `json:"visible_at"`
}

// InputInvalidFact is a bounded operator-facing view of one
// durable reducer_input_invalid_facts row (issue #4630): a fact the reducer
// quarantined during typed-payload decode because a required field was
// missing or null.
type InputInvalidFact struct {
	FactID       string    `json:"fact_id"`
	FactKind     string    `json:"fact_kind"`
	MissingField string    `json:"missing_field"`
	FailureClass string    `json:"failure_class"`
	Domain       string    `json:"domain"`
	ScopeID      string    `json:"scope_id"`
	GenerationID string    `json:"generation_id"`
	DecidedAt    time.Time `json:"decided_at"`
}

// InputInvalidFactListFilter constrains bounded reducer_input_invalid_facts
// read queries. ScopeID and GenerationID are required: the table is always
// read scoped to one ingestion scope generation (mirrors
// AdmissionDecisionReadFilter and DeadLetterListFilter's scope requirement).
// AllowedRepositoryIDs and AllowedScopeIDs carry a scoped token's grants so
// the store query can authorize the requested ScopeID against
// ingestion_scopes (mirroring DeadLetterListFilter): a repository-scoped
// token that grants a repository but not the raw ingestion scope_id must
// still be authorized when the requested ScopeID belongs to that repository
// (codex review on PR #5252, issue #4630).
type InputInvalidFactListFilter struct {
	ScopeID              string
	GenerationID         string
	Domain               string
	FactKind             string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	Limit                int
	Timeout              time.Duration
}

// ReplayEvent is an admin-friendly view of a fact_replay_events row.
type ReplayEvent struct {
	ReplayEventID string    `json:"replay_event_id"`
	WorkItemID    string    `json:"work_item_id"`
	ScopeID       string    `json:"scope_id"`
	GenerationID  string    `json:"generation_id"`
	FailureClass  *string   `json:"failure_class"`
	OperatorNote  *string   `json:"operator_note"`
	CreatedAt     time.Time `json:"created_at"`
}

// BackfillRequest is an admin-friendly view of a fact_backfill_requests row.
type BackfillRequest struct {
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

// DeadLetterListFilter constrains bounded dead-letter read queries.
type DeadLetterListFilter struct {
	FailureClass         string
	Domain               string
	ScopeID              string
	CollectorKind        string
	UpdatedAfter         *time.Time
	UpdatedBefore        *time.Time
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	Limit                int
	Timeout              time.Duration
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
	// ExcludeFailureClasses skips terminal work items whose failure_class is in
	// this set. The replay handler populates it with the unsafe-to-replay
	// classes unless the operator forces the replay.
	ExcludeFailureClasses []string
}

// UnsafeReplayTargetFilter selects the replay-eligible terminal work items,
// among an explicit id list, whose failure_class is in an unsafe or
// manual-review class. It carries the same narrowing selectors the replay
// itself applies so the read and the replay agree on the candidate set.
type UnsafeReplayTargetFilter struct {
	WorkItemIDs []string
	ScopeID     string
	Stage       string
	// FailureClass is the request's failure_class selector. The replay ANDs it
	// with the id list, so the read must too or it would refuse rows the replay
	// would never select.
	FailureClass string
	// UnsafeFailureClasses is the set of failure classes that must not be
	// replayed without force.
	UnsafeFailureClasses []string
}

// UnsafeReplayTarget names one replay-eligible terminal work item that sits in
// an unsafe or manual-review failure class.
type UnsafeReplayTarget struct {
	WorkItemID   string
	FailureClass string
}

// SupersededReplayTarget names one replay-eligible terminal projector work
// item, among an explicit id list, whose scope generation is superseded
// (#7130). The store never replays such a row, because projecting it would
// re-project the retired generation's graph and content over the published
// one.
type SupersededReplayTarget struct {
	WorkItemID   string
	GenerationID string
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

// ReplayIdempotencyClaim is the outcome of attempting to claim a replay
// idempotency key. Exactly one concurrent request observes Claimed=true and
// executes the replay; every other request observes the existing ledger row.
// The type lives with the Store port it is claimed through; the ledger
// itself is implemented in store/.
type ReplayIdempotencyClaim struct {
	// Claimed is true when this request won the claim and must run the replay.
	Claimed bool
	// Status is the existing row status when not claimed (in_progress|completed).
	Status string
	// Fingerprint is the existing row's request fingerprint when not claimed,
	// used to reject a reused key carrying different selectors.
	Fingerprint string
	// ReplayedCount is the recorded outcome count when Status is completed.
	ReplayedCount int
	// WorkItemIDs is the recorded outcome id list when Status is completed.
	WorkItemIDs []string
}

// Handler provides HTTP endpoints for administrative operations
// including recovery, work-item inspection, and fact-queue management.
type Handler struct {
	Recovery  RecoveryService
	Reindexer ReindexRequester
	// Repositories and RepositoryReindexer serve repository-scoped reindex
	// requests (#7620); either nil makes them 503.
	Repositories        RepositoryCatalogMatcher
	RepositoryReindexer RepositoryReindexRequester
	Store               Store
	// ReadStore serves inspections; nil preserves legacy single-store wiring.
	ReadStore ReadStore
	// Audit records governance audit events for mutating recovery actions.
	// A nil appender disables audit recording (the action still proceeds).
	Audit audit.Appender
	// Clock supplies the current time for idempotency bookkeeping; nil uses
	// time.Now().UTC().
	Clock func() time.Time
	// Instruments records query duration and error telemetry for
	// listInputInvalidFacts (issue #4630). Nil disables that telemetry; the
	// route itself is unaffected.
	Instruments *telemetry.Instruments
}

// readStore chooses the configured inspection port before any query runs.
// A read error never triggers a fallback to the writer-backed Store.
func (h *Handler) readStore() ReadStore {
	if h.ReadStore != nil {
		return h.ReadStore
	}
	return h.Store
}

// now returns the handler clock, defaulting to the wall clock.
func (h *Handler) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now().UTC()
}

// Mount registers all admin routes on the given mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	// Core admin endpoints (from admin.py)
	mux.HandleFunc("POST /api/v0/admin/refinalize", h.refinalize)
	mux.HandleFunc("GET /api/v0/admin/shared-projection/tuning-report", h.tuningReport)
	mux.HandleFunc("POST /api/v0/admin/reindex", h.reindex)
	mux.HandleFunc("POST /api/v0/admin/recover-generations", h.recoverGenerations)

	// Fact-inspection endpoints (from admin_facts.py)
	mux.HandleFunc("POST /api/v0/admin/work-items/query", h.listWorkItems)
	mux.HandleFunc("POST /api/v0/admin/dead-letters/query", h.listDeadLetters)
	mux.HandleFunc("POST /api/v0/admin/input-invalid-facts/query", h.listInputInvalidFacts)
	mux.HandleFunc("POST /api/v0/admin/changed-since/poisoned-links/query", h.listChangedSincePoisonedLinks)
	mux.HandleFunc("POST /api/v0/admin/decisions/query", h.listDecisions)
	mux.HandleFunc("POST /api/v0/admin/dead-letter", h.deadLetter)
	mux.HandleFunc("POST /api/v0/admin/skip", h.skip)
	mux.HandleFunc("POST /api/v0/admin/replay", h.replay)
	mux.HandleFunc("POST /api/v0/admin/reopen", h.reopen)
	mux.HandleFunc("POST /api/v0/admin/backfill", h.backfill)
	mux.HandleFunc("POST /api/v0/admin/replay-events/query", h.listReplayEvents)
}

// refinalize re-enqueues projector work for the given scope IDs.
// POST /api/v0/admin/refinalize
func (h *Handler) refinalize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScopeIDs []string `json:"scope_ids"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.ScopeIDs) == 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_ids is required and must not be empty")
		return
	}
	if h.Recovery == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "recovery handler not configured")
		return
	}

	result, err := h.Recovery.Refinalize(r.Context(), recovery.RefinalizeFilter{
		ScopeIDs: req.ScopeIDs,
	})
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.reportRefinalizeOutcome(r.Context(), "refinalize", "scope_ids", result)
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"status":                               "accepted",
		"enqueued":                             result.Enqueued,
		"scope_ids":                            result.ScopeIDs,
		"reducer_work_deleted":                 result.ReducerWorkDeleted,
		"shared_intents_reopened":              result.SharedIntentsReopened,
		"readiness_phases_cleared":             result.ReadinessPhasesCleared,
		"generations_retired":                  result.GenerationsRetired,
		"shared_projection_acceptance_cleared": result.SharedProjectionAcceptanceCleared,
		"skipped_scopes":                       skippedScopesResponse(result.Skipped),
		"delta_active_scopes":                  result.DeltaActive.Report(),
		"reindex_requests_written":             result.DeltaActive.ReindexRequestsWritten(),
	})
}

// tuningReport returns shared-projection tuning information.
// GET /api/v0/admin/shared-projection/tuning-report
func (h *Handler) tuningReport(w http.ResponseWriter, _ *http.Request) {
	// Shared-write tuning is managed by the projector pipeline internally.
	// This endpoint returns a static report indicating the Go-owned surface.
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "not_applicable",
		"detail": "Shared-projection tuning is managed internally by the Go projector pipeline.",
	})
}
