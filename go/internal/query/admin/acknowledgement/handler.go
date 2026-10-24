// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// route is the one route this leaf serves.
const route = "/api/v0/admin/collector-terminal-failure-acknowledgements"

// acknowledgeFailedMessage is the fixed body of a failed acknowledgement
// write (#7674). The store error quotes SQL and connection details, so it
// never reaches the body; the request span carries the cause.
const acknowledgeFailedMessage = "collector terminal failure acknowledgement failed"

// Outcomes of the acknowledgement counter, a closed set (#8830).
const (
	outcomeRecorded     = "recorded"
	outcomeUnchanged    = "unchanged"
	outcomeRefused      = "refused"
	outcomeNotFound     = "not_found"
	outcomeClassChanged = "class_changed"
	outcomeFailed       = "failed"
)

// Acknowledger records an acknowledgement. pgstorage.TerminalFailureAckStore
// satisfies it.
type Acknowledger interface {
	Acknowledge(ctx context.Context, req pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckResult, error)
}

// Handler serves the acknowledgement route.
type Handler struct {
	// Acknowledger writes the acknowledgement; nil makes the route 503.
	Acknowledger Acknowledger
	// Audit records one governance audit event per decision. A nil appender
	// disables audit recording; the acknowledgement still proceeds.
	Audit audit.Appender
	// Instruments counts outcomes; nil disables the counter.
	Instruments *telemetry.Instruments
	// Logger defaults to the process default logger.
	Logger *slog.Logger
	// Clock supplies the time of audit events; nil uses time.Now().UTC().
	Clock func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now().UTC()
}

func (h *Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// Mount registers the acknowledgement route on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST "+route, h.acknowledge)
}

// acknowledgeRequest is the operator's body: the instance and scope that
// failed, the failure class the operator read from collector readiness, and
// the reason. The class makes the acknowledgement name the failure the
// operator saw: a failure that changed class in between is refused.
type acknowledgeRequest struct {
	InstanceID   string `json:"instance_id"`
	ScopeID      string `json:"scope_id"`
	FailureClass string `json:"failure_class"`
	Reason       string `json:"reason"`
}

// acknowledge validates the request, authorizes the caller, and records the
// acknowledgement. The write is one idempotent upsert: a repeat of a covered
// failure answers 200 with duplicate true and writes nothing, and a client
// cancel after the upsert committed answers 499 with the acknowledgement
// durable, so re-running the request is safe.
func (h *Handler) acknowledge(w http.ResponseWriter, r *http.Request) {
	if h.Acknowledger == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "collector terminal failure acknowledgement is not configured")
		return
	}
	var req acknowledgeRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.InstanceID = strings.TrimSpace(req.InstanceID)
	req.ScopeID = strings.TrimSpace(req.ScopeID)
	req.FailureClass = strings.TrimSpace(req.FailureClass)
	req.Reason = strings.TrimSpace(req.Reason)
	authCtx, _ := auth.AuthContextFromContext(r.Context())
	correlationID := audit.SafeCorrelationID(audit.CorrelationID(r))

	if req.Reason == "" {
		h.deny(r.Context(), outcomeRefused, "collector_failure_ack_refused_missing_reason", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest, "reason is required and must say why the failure is expected")
		return
	}
	if req.InstanceID == "" || req.ScopeID == "" || req.FailureClass == "" ||
		utf8.RuneCountInString(req.Reason) > pgstorage.MaxTerminalFailureAckReasonLength {
		h.deny(r.Context(), outcomeRefused, "collector_failure_ack_refused_invalid_request", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest,
			"instance_id, scope_id, and failure_class are required, and reason is at most 512 characters")
		return
	}
	// Authorization mirrors replay and recover-generations: an admin
	// (all-scopes) principal may acknowledge, unauthenticated dev mode
	// (auth.Mode == "") is intentionally open, and a scoped token is denied.
	if authCtx.Mode != "" && !authCtx.AllScopes {
		h.deny(r.Context(), outcomeRefused, "collector_failure_ack_refused_unauthorized", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusForbidden, "acknowledging a collector terminal failure requires an admin (all-scopes) token")
		return
	}

	actorClass, actorIDHash := audit.RecoveryActor(authCtx)
	result, err := h.Acknowledger.Acknowledge(r.Context(), pgstorage.TerminalFailureAckRequest{
		InstanceID: req.InstanceID, ScopeID: req.ScopeID, FailureClass: req.FailureClass,
		Reason: req.Reason, ActorClass: string(actorClass), ActorIDHash: actorIDHash,
	})
	if err != nil {
		h.answerError(w, r, err, authCtx, correlationID)
		return
	}

	outcome, status, reason := outcomeRecorded, "acknowledged", "collector_failure_ack_recorded"
	if result.Unchanged {
		outcome, status, reason = outcomeUnchanged, "unchanged", "collector_failure_ack_unchanged"
	}
	h.count(r.Context(), outcome)
	h.record(r.Context(), governanceaudit.DecisionAllowed, reason, authCtx, correlationID)
	h.logger().InfoContext(r.Context(), "collector terminal failure acknowledged",
		slog.String("outcome", outcome),
		slog.String("collector_kind", result.CollectorKind),
		slog.String("instance_id", result.InstanceID),
		slog.String("failure_class", result.FailureClass),
	)
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"status":    status,
		"duplicate": result.Unchanged,
		"acknowledgement": map[string]any{
			"collector_kind":  result.CollectorKind,
			"instance_id":     result.InstanceID,
			"scope_id":        result.ScopeID,
			"failure_class":   result.FailureClass,
			"failure_at":      result.FailureAt.UTC().Format(time.RFC3339),
			"acknowledged_at": result.AcknowledgedAt.UTC().Format(time.RFC3339),
			"reason":          result.Reason,
		},
	})
}

// answerError maps a store error to its answer: the three refusals are
// client errors with their own audit reasons, and anything else is a server
// fault with the fixed body (#7674).
func (h *Handler) answerError(w http.ResponseWriter, r *http.Request, err error, authCtx auth.AuthContext, correlationID string) {
	switch {
	case errors.Is(err, pgstorage.ErrTerminalFailureAckInstanceNotFound):
		h.deny(r.Context(), outcomeNotFound, "collector_failure_ack_refused_instance_not_found", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusNotFound, "collector instance is not registered or not active")
	case errors.Is(err, pgstorage.ErrTerminalFailureAckNoFailure):
		h.deny(r.Context(), outcomeNotFound, "collector_failure_ack_refused_no_failure", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusNotFound, "no unresolved terminal failure on that instance and scope")
	case errors.Is(err, pgstorage.ErrTerminalFailureAckClassChanged):
		h.deny(r.Context(), outcomeClassChanged, "collector_failure_ack_refused_class_changed", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusConflict, "the unresolved terminal failure has a different class than failure_class; read collector readiness again")
	default:
		h.deny(r.Context(), outcomeFailed, "collector_failure_ack_failed", authCtx, correlationID)
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, acknowledgeFailedMessage)
	}
}

// count moves the outcome counter once per request. Nil instruments are a
// no-op.
func (h *Handler) count(ctx context.Context, outcome string) {
	if h.Instruments == nil || h.Instruments.CollectorTerminalFailureAcknowledgements == nil {
		return
	}
	h.Instruments.CollectorTerminalFailureAcknowledgements.Add(ctx, 1,
		metric.WithAttributes(telemetry.AttrOutcome(outcome)))
}

// deny counts a refused or failed request and records the denial.
func (h *Handler) deny(ctx context.Context, outcome, reasonCode string, authCtx auth.AuthContext, correlationID string) {
	h.count(ctx, outcome)
	h.record(ctx, governanceaudit.DecisionDenied, reasonCode, authCtx, correlationID)
}

// record appends one governance audit event for a decision. It never blocks
// the action and carries only the bounded reason code, the actor class, and
// hashes: never the operator's reason, the instance, or the scope.
func (h *Handler) record(ctx context.Context, decision governanceaudit.Decision, reasonCode string, authCtx auth.AuthContext, correlationID string) {
	if h.Audit == nil {
		return
	}
	actorClass, actorIDHash := audit.RecoveryActor(authCtx)
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeAdminRecoveryAction,
		ActorClass:         actorClass,
		ActorIDHash:        actorIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           decision,
		ReasonCode:         reasonCode,
		CorrelationID:      correlationID,
		PolicyRevisionHash: authCtx.PolicyRevisionHash,
		OccurredAt:         h.now(),
	}
	appendCtx, cancel := context.WithTimeout(ctx, audit.AppendTimeout)
	defer cancel()
	_ = h.Audit.Append(appendCtx, []governanceaudit.Event{event})
}
