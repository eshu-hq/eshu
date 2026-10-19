// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// RecoveryHandler provides HTTP endpoints for write-plane recovery operations.
// It replaces the Python admin refinalize and replay surfaces with Go-owned
// queue replay rather than direct graph mutation.
type RecoveryHandler struct {
	handler *recovery.Handler
}

// NewRecoveryHandler constructs the HTTP recovery handler.
func NewRecoveryHandler(handler *recovery.Handler) (*RecoveryHandler, error) {
	if handler == nil {
		return nil, fmt.Errorf("recovery handler is required")
	}

	return &RecoveryHandler{handler: handler}, nil
}

// Mount registers recovery routes on the given mux.
func (h *RecoveryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/admin/replay", h.handleReplay)
	mux.HandleFunc("/admin/replay-collector-generations", h.handleReplayCollectorGenerations)
	mux.HandleFunc("/admin/refinalize", h.handleRefinalize)
}

// Fixed client-facing messages for the recovery admin routes (#8394). A store
// error quotes SQL, hosts, and constraint names, so err.Error() never reaches
// a response body: each route answers exactly one of these. The cause is
// recorded on the request span and logged server-side instead.
const (
	// replayFailedMessage answers a failed /admin/replay store read or write.
	replayFailedMessage = "replay failed"

	// collectorGenerationReplayFailedMessage answers a failed
	// /admin/replay-collector-generations store read or write.
	collectorGenerationReplayFailedMessage = "collector generation replay failed"

	// refinalizeFailedMessage answers a failed /admin/refinalize store
	// transaction.
	refinalizeFailedMessage = "refinalize failed"
)

type replayRequest struct {
	Stage        string   `json:"stage"`
	ScopeIDs     []string `json:"scope_ids"`
	FailureClass string   `json:"failure_class"`
	Limit        int      `json:"limit"`
}

type replayResponse struct {
	Status      string   `json:"status"`
	Stage       string   `json:"stage"`
	Replayed    int      `json:"replayed"`
	WorkItemIDs []string `json:"work_item_ids"`
	// SkippedSupersededGeneration counts projector rows left terminal because
	// their generation is superseded (#7130).
	SkippedSupersededGeneration int `json:"skipped_superseded_generation"`
}

// handleReplay replays failed projector or reducer work items.
func (h *RecoveryHandler) handleReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req replayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	filter := recovery.ReplayFilter{
		Stage:        recovery.Stage(req.Stage),
		ScopeIDs:     req.ScopeIDs,
		FailureClass: req.FailureClass,
		Limit:        req.Limit,
	}

	// A filter rejection is caller-actionable, so it keeps its text (422).
	// Anything the domain reports past this point is a store failure: it
	// answers the fixed message, never the store error text (#8394).
	if err := filter.Validate(); err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	result, err := h.handler.ReplayFailed(r.Context(), filter)
	if err != nil {
		writeRecoveryFailure(w, r, err, replayFailedMessage)
		return
	}

	writeJSON(w, http.StatusOK, replayResponse{
		Status:      "replayed",
		Stage:       string(result.Stage),
		Replayed:    result.Replayed,
		WorkItemIDs: result.WorkItemIDs,

		SkippedSupersededGeneration: result.SkippedSupersededGeneration,
	})
}

type collectorGenerationReplayRequest struct {
	ScopeIDs      []string `json:"scope_ids"`
	FailureClass  string   `json:"failure_class"`
	CollectorKind string   `json:"collector_kind"`
	Limit         int      `json:"limit"`
}

type collectorGenerationReplayResponse struct {
	Status        string   `json:"status"`
	Replayed      int      `json:"replayed"`
	GenerationIDs []string `json:"generation_ids"`
}

// handleReplayCollectorGenerations marks failed collector generations for
// source-level replay.
func (h *RecoveryHandler) handleReplayCollectorGenerations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req collectorGenerationReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	collectorFilter := recovery.CollectorGenerationReplayFilter{
		ScopeIDs:      req.ScopeIDs,
		FailureClass:  req.FailureClass,
		CollectorKind: req.CollectorKind,
		Limit:         req.Limit,
	}

	// A filter rejection is caller-actionable, so it keeps its text (422).
	// Anything the domain reports past this point is a store failure: it
	// answers the fixed message, never the store error text (#8394).
	if err := collectorFilter.Validate(); err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	result, err := h.handler.ReplayCollectorGenerations(r.Context(), collectorFilter)
	if err != nil {
		writeRecoveryFailure(w, r, err, collectorGenerationReplayFailedMessage)
		return
	}

	writeJSON(w, http.StatusOK, collectorGenerationReplayResponse{
		Status:        "replay_requested",
		Replayed:      result.Replayed,
		GenerationIDs: result.GenerationIDs,
	})
}

type refinalizeRequest struct {
	ScopeIDs []string `json:"scope_ids"`
}

// refinalizeResponse reports the re-enqueue plus the dedup state the refinalize
// cleared for those scopes. The five reset counts matter here as much as on the
// disaster-recovery route: re-projecting a wedged scope without re-driving its
// reducer domains rebuilds only the source-local half of what that scope owns.
type refinalizeResponse struct {
	Status                            string   `json:"status"`
	Enqueued                          int      `json:"enqueued"`
	ScopeIDs                          []string `json:"scope_ids"`
	ReducerWorkDeleted                int      `json:"reducer_work_deleted"`
	SharedIntentsReopened             int      `json:"shared_intents_reopened"`
	ReadinessPhasesCleared            int      `json:"readiness_phases_cleared"`
	GenerationsRetired                int      `json:"generations_retired"`
	SharedProjectionAcceptanceCleared int      `json:"shared_projection_acceptance_cleared"`

	// SkippedScopes reports the scopes the refinalize considered but did not
	// re-enqueue, by reason (#7116), so a partial rebuild is visible.
	SkippedScopes recovery.SkippedScopesReport `json:"skipped_scopes"`

	// DeltaActiveScopes reports the re-enqueued scopes whose generation is a
	// delta, by outcome (#7797): the graph for them stays incomplete until a
	// full generation activates.
	DeltaActiveScopes recovery.DeltaActiveScopesReport `json:"delta_active_scopes"`

	// ReindexRequestsWritten reports the per-repository reindex watermarks
	// the refinalize wrote in its transaction (#7797).
	ReindexRequestsWritten recovery.ReindexRequestsWrittenReport `json:"reindex_requests_written"`
}

// handleRefinalize re-enqueues projector work for the specified scopes.
func (h *RecoveryHandler) handleRefinalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req refinalizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	filter := recovery.RefinalizeFilter{
		ScopeIDs: req.ScopeIDs,
	}

	// A filter rejection is caller-actionable, so it keeps its text (422).
	// Anything the domain reports past this point is a store failure: it
	// answers the fixed message, never the store error text (#8394).
	if err := filter.Validate(); err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	result, err := h.handler.Refinalize(r.Context(), filter)
	if err != nil {
		// Claim-fence contention is a retry verdict, not a fault: it keeps
		// its 409 signal with the fixed sentinel text, matching the
		// /api/v0/admin/refinalize surface.
		if errors.Is(err, recovery.ErrRefinalizeClaimFenceBusy) {
			writeJSONError(w, http.StatusConflict, recovery.ErrRefinalizeClaimFenceBusy.Error())
			return
		}
		writeRecoveryFailure(w, r, err, refinalizeFailedMessage)
		return
	}

	writeJSON(w, http.StatusOK, refinalizeResponse{
		Status:                            "enqueued",
		Enqueued:                          result.Enqueued,
		ScopeIDs:                          result.ScopeIDs,
		ReducerWorkDeleted:                result.ReducerWorkDeleted,
		SharedIntentsReopened:             result.SharedIntentsReopened,
		ReadinessPhasesCleared:            result.ReadinessPhasesCleared,
		GenerationsRetired:                result.GenerationsRetired,
		SharedProjectionAcceptanceCleared: result.SharedProjectionAcceptanceCleared,
		SkippedScopes:                     result.Skipped.Report(),
		DeltaActiveScopes:                 result.DeltaActive.Report(),
		ReindexRequestsWritten:            result.DeltaActive.ReindexRequestsWritten(),
	})
}

type jsonErrorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, jsonErrorResponse{Error: message})
}

// writeRecoveryFailure answers a failed recovery store call with the fixed
// message body, so SQL, hosts, and constraint names never reach the client
// (#8394). The response envelope stays the JSON error the routes always
// answered; only the message is fixed and the status now names the cause.
//
// message is the only text the client sees: never pass err.Error() or a string
// formatted with err. A server fault answers 500, records err on the request
// span (exception event and Error status), and logs err server-side so an
// operator can find it.
//
// A client cancel answers statuspkg.StatusClientClosedRequest (499) with the
// same fixed body: err is not recorded or logged, the span status stays unset,
// and only the client-canceled event is added. It counts as a client cancel
// only when err wraps context.Canceled AND r.Context() itself is canceled; a
// context.Canceled from some inner context on a live request is a fault. This
// mirrors status.WriteReadFailure, which the recovery routes cannot reuse
// directly because it answers a plain-text body.
func writeRecoveryFailure(w http.ResponseWriter, r *http.Request, err error, message string) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	if errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
		span.AddEvent(statuspkg.ClientCanceledEvent)
		writeJSONError(w, statuspkg.StatusClientClosedRequest, message)
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, message)
	slog.ErrorContext(ctx, message, "path", r.URL.Path, "error", err)
	writeJSONError(w, http.StatusInternalServerError, message)
}
