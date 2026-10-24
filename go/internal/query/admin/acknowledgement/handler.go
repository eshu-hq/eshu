// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Routes this leaf serves.
const (
	acknowledgeRoute = "/api/v0/admin/collector-failures/acknowledge"
	queryRoute       = "/api/v0/admin/collector-failures/acknowledgements/query"
)

// Fixed client-facing messages (#7674). The response body is exactly this
// text: a store error quotes SQL, relation names, hosts, and connection
// settings, so err.Error() never reaches a 5xx body. The cause is recorded on
// the request span.
const (
	acknowledgeFailedMessage = "collector terminal failure acknowledgement failed"
	claimFailedMessage       = "collector terminal failure acknowledgement idempotency claim failed"
	recordFailedMessage      = "collector terminal failure acknowledged but recording its idempotency outcome failed"
	queryFailedMessage       = "collector terminal failure acknowledgement query failed"
)

// Outcomes of the acknowledgement counter, a closed set (#8830).
const (
	outcomeAcknowledged = "acknowledged"
	outcomeDuplicate    = "duplicate"
	outcomeRefused      = "refused"
)

// defaultFinalizeTimeout bounds the idempotency finalize and the accepted
// audit append, which run on a context a client cancel cannot abort.
const defaultFinalizeTimeout = 2 * time.Second

// Acknowledger resolves and records acknowledgements and lists them for the
// admin read. pgstorage.TerminalFailureAckStore satisfies it.
type Acknowledger interface {
	ResolveTarget(ctx context.Context, req pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckTarget, error)
	Acknowledge(ctx context.Context, req pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckResult, error)
	ListTerminalFailureAcks(ctx context.Context, filter pgstorage.TerminalFailureAckListFilter) ([]pgstorage.TerminalFailureAckRecord, error)
}

// Ledger is the replay idempotency ledger (admin_replay_requests) the replay,
// reopen, and recover-generations routes share. The admin Store satisfies it.
type Ledger interface {
	ClaimReplayIdempotency(ctx context.Context, key, fingerprint string, now time.Time) (admin.ReplayIdempotencyClaim, error)
	CompleteReplayIdempotency(ctx context.Context, key string, count int, workItemIDs []string, now time.Time) error
}

// Handler serves the acknowledgement routes.
type Handler struct {
	// Acknowledger writes and reads acknowledgements; nil makes the routes 503.
	Acknowledger Acknowledger
	// Ledger claims and completes the idempotency key; nil makes the write 503.
	Ledger Ledger
	// Audit records one governance audit event per decision. A nil appender
	// disables audit recording; the acknowledgement still proceeds.
	Audit audit.Appender
	// Instruments counts outcomes; nil disables the counter.
	Instruments *telemetry.Instruments
	// Logger defaults to the process default logger.
	Logger *slog.Logger
	// Clock supplies the time of audit events and ledger bookkeeping; nil uses
	// time.Now().UTC().
	Clock func() time.Time
	// FinalizeTimeout bounds the ledger finalize and the accepted audit append
	// that run after the acknowledgement committed. Zero uses 2s.
	FinalizeTimeout time.Duration
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

// Mount registers the acknowledgement routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST "+acknowledgeRoute, h.acknowledge)
	mux.HandleFunc("POST "+queryRoute, h.queryAcknowledgements)
}

// acknowledgeRequest is the operator's body: the failed target, the reason, and
// the idempotency key. failure_class is optional: when set, the acknowledgement
// is refused (409) if the failure on the scope has a different bounded class,
// so an operator who read class X never acknowledges a class they have not seen.
type acknowledgeRequest struct {
	CollectorKind  string `json:"collector_kind"`
	InstanceID     string `json:"instance_id"`
	ScopeID        string `json:"scope_id"`
	FailureClass   string `json:"failure_class"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (r *acknowledgeRequest) normalize() {
	r.CollectorKind = strings.TrimSpace(r.CollectorKind)
	r.InstanceID = strings.TrimSpace(r.InstanceID)
	r.ScopeID = strings.TrimSpace(r.ScopeID)
	r.FailureClass = strings.TrimSpace(r.FailureClass)
	r.Reason = strings.TrimSpace(r.Reason)
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
}

// acknowledge validates the request, authorizes the caller, resolves the
// failure, claims the idempotency key, and records the acknowledgement. It
// follows the reopen contract: every expected refusal happens before the claim,
// so a refused request leaves its key unconsumed for a corrected retry.
func (h *Handler) acknowledge(w http.ResponseWriter, r *http.Request) {
	if h.Acknowledger == nil || h.Ledger == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "collector terminal failure acknowledgement is not configured")
		return
	}
	var req acknowledgeRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.normalize()
	authCtx, _ := auth.AuthContextFromContext(r.Context())
	correlationID := audit.SafeCorrelationID(audit.CorrelationID(r))

	if req.CollectorKind == "" || req.InstanceID == "" || req.ScopeID == "" ||
		utf8.RuneCountInString(req.Reason) > pgstorage.MaxTerminalFailureAckReasonLength {
		h.refuse(r.Context(), "ack_refused_invalid_request", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest,
			"collector_kind, instance_id, and scope_id are required, and reason is at most 512 characters")
		return
	}
	if req.Reason == "" {
		h.refuse(r.Context(), "ack_refused_missing_reason", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest, "reason is required and must say why the failure is expected")
		return
	}
	if req.IdempotencyKey == "" {
		h.refuse(r.Context(), "ack_refused_missing_idempotency_key", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest, "idempotency_key is required to make acknowledgement safe under retries")
		return
	}
	// Authorization mirrors replay, reopen, and recover-generations: an admin
	// (all-scopes) principal may acknowledge, unauthenticated dev mode
	// (auth.Mode == "") is intentionally open, and a scoped token is denied.
	if authCtx.Mode != "" && !authCtx.AllScopes {
		h.refuse(r.Context(), "ack_refused_unauthorized", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusForbidden, "acknowledging a collector terminal failure requires an admin (all-scopes) token")
		return
	}

	actorClass, actorIDHash := audit.RecoveryActor(authCtx)
	storeReq := pgstorage.TerminalFailureAckRequest{
		CollectorKind: req.CollectorKind, InstanceID: req.InstanceID, ScopeID: req.ScopeID,
		FailureClass: req.FailureClass, Reason: req.Reason,
		ActorClass: string(actorClass), ActorIDHash: actorIDHash, CorrelationID: correlationID,
	}

	// Resolve before the claim: an unknown instance, a scope with nothing to
	// acknowledge, or a changed class is an expected, side-effect-free refusal,
	// and refusing it here leaves the idempotency key unconsumed. The store
	// re-resolves authoritatively inside the acknowledgement transaction.
	if _, err := h.Acknowledger.ResolveTarget(r.Context(), storeReq); err != nil {
		h.answerStoreError(w, r, err, authCtx, correlationID)
		return
	}

	fingerprint := ackFingerprint(req.CollectorKind, req.InstanceID, req.ScopeID)
	claim, err := h.Ledger.ClaimReplayIdempotency(r.Context(), req.IdempotencyKey, fingerprint, h.now())
	if err != nil {
		// One INSERT ... ON CONFLICT DO NOTHING: a cancel observed after the
		// insert committed leaves the key in_progress with nothing
		// acknowledged, and a retry with the same key answers 409.
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, claimFailedMessage)
		return
	}
	if !claim.Claimed {
		h.respondDuplicate(w, r, req, claim, fingerprint, authCtx, correlationID)
		return
	}

	result, err := h.Acknowledger.Acknowledge(r.Context(), storeReq)
	if err != nil {
		// One transaction with a deferred rollback: an error return means
		// rolled back, except a commit-time error or a client cancel that
		// lands while the commit itself runs, where the acknowledgement may be
		// durable. The ledger row stays in_progress either way, so a retry
		// answers 409 instead of losing the outcome. A refusal here is a race
		// with the pre-claim resolve; the key is then consumed.
		h.answerStoreError(w, r, err, authCtx, correlationID)
		return
	}

	// The acknowledgement committed: finalize on a context a client cancel
	// cannot abort, so the key never strands in_progress.
	finalizeCtx, cancel := h.finalizeContext(r)
	defer cancel()
	if err := h.Ledger.CompleteReplayIdempotency(finalizeCtx, req.IdempotencyKey, 1, encodeOutcome(result), h.now()); err != nil {
		// Only the ledger finalize failed; the row stays in_progress (retry
		// returns 409). Always a server fault, client cancel or not.
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, recordFailedMessage)
		return
	}
	h.count(finalizeCtx, outcomeAcknowledged)
	h.record(finalizeCtx, governanceaudit.DecisionAllowed, "ack_recorded", authCtx, correlationID)
	h.logger().InfoContext(finalizeCtx, "collector terminal failure acknowledged",
		slog.String("outcome", outcomeAcknowledged),
		slog.String("collector_kind", result.CollectorKind),
		slog.String("instance_id", result.InstanceID),
		slog.String("failure_class", result.FailureClass),
	)
	querycontract.WriteJSON(w, http.StatusOK, responseBody("acknowledged", req.IdempotencyKey, result))
}

// respondDuplicate answers a key that was already claimed: a different target
// is a 409, a claim still in progress is a 409, and a completed one replays its
// stored outcome with status duplicate.
func (h *Handler) respondDuplicate(
	w http.ResponseWriter,
	r *http.Request,
	req acknowledgeRequest,
	claim admin.ReplayIdempotencyClaim,
	fingerprint string,
	authCtx auth.AuthContext,
	correlationID string,
) {
	if claim.Fingerprint != "" && claim.Fingerprint != fingerprint {
		h.refuse(r.Context(), "ack_idempotency_key_reused", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusConflict, "idempotency_key was already used with a different collector_kind, instance_id, or scope_id")
		return
	}
	if claim.Status != admin.ReplayRequestStatusCompleted {
		querycontract.WriteError(w, http.StatusConflict, "an acknowledgement for this idempotency_key is already in progress")
		return
	}
	h.count(r.Context(), outcomeDuplicate)
	h.record(r.Context(), governanceaudit.DecisionAllowed, "ack_idempotent_replay", authCtx, correlationID)
	body := map[string]any{"status": "duplicate", "idempotency_key": req.IdempotencyKey}
	if stored, ok := decodeOutcome(claim.WorkItemIDs); ok {
		body = responseBody("duplicate", req.IdempotencyKey, stored)
	}
	querycontract.WriteJSON(w, http.StatusOK, body)
}

// answerStoreError maps a store error to its answer: the three refusals are
// client errors with their own audit reasons, and anything else is a server
// fault with the fixed body (#7674).
func (h *Handler) answerStoreError(w http.ResponseWriter, r *http.Request, err error, authCtx auth.AuthContext, correlationID string) {
	switch {
	case errors.Is(err, pgstorage.ErrTerminalFailureAckInstanceNotFound):
		h.refuse(r.Context(), "ack_refused_instance_not_found", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusNotFound, "collector instance is not registered or not active for that collector_kind")
	case errors.Is(err, pgstorage.ErrTerminalFailureAckNoFailure):
		h.refuse(r.Context(), "ack_refused_no_terminal_failure", authCtx, correlationID)
		querycontract.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"status": "refused",
			"reason": "no unresolved terminal failure inside the window on that instance and scope",
			"detail": "nothing was acknowledged and the idempotency key was not consumed",
		})
	case errors.Is(err, pgstorage.ErrTerminalFailureAckClassChanged):
		h.refuse(r.Context(), "ack_refused_class_changed", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusConflict, "the unresolved terminal failure has a different class than failure_class; read collector readiness again")
	default:
		h.record(r.Context(), governanceaudit.DecisionDenied, "ack_failed", authCtx, correlationID)
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, acknowledgeFailedMessage)
	}
}

// queryRequest filters the admin read. All fields are optional.
type queryRequest struct {
	CollectorKind string `json:"collector_kind"`
	InstanceID    string `json:"instance_id"`
	ScopeID       string `json:"scope_id"`
	Limit         int    `json:"limit"`
}

// queryAcknowledgements is the admin-only read that returns the operator's
// reason. No status surface returns it.
func (h *Handler) queryAcknowledgements(w http.ResponseWriter, r *http.Request) {
	if h.Acknowledger == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "collector terminal failure acknowledgement is not configured")
		return
	}
	var req queryRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	authCtx, _ := auth.AuthContextFromContext(r.Context())
	if authCtx.Mode != "" && !authCtx.AllScopes {
		querycontract.WriteError(w, http.StatusForbidden, "reading collector terminal failure acknowledgements requires an admin (all-scopes) token")
		return
	}
	records, err := h.Acknowledger.ListTerminalFailureAcks(r.Context(), pgstorage.TerminalFailureAckListFilter{
		CollectorKind: req.CollectorKind, InstanceID: req.InstanceID, ScopeID: req.ScopeID, Limit: req.Limit,
	})
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, queryFailedMessage)
		return
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"collector_kind":  record.CollectorKind,
			"instance_id":     record.InstanceID,
			"scope_id":        record.ScopeID,
			"failure_class":   record.FailureClass,
			"failed_at":       record.FailureAt.UTC().Format(time.RFC3339),
			"work_item_id":    record.WorkItemID,
			"reason":          record.Reason,
			"actor_class":     record.ActorClass,
			"correlation_id":  record.CorrelationID,
			"acknowledged_at": record.AcknowledgedAt.UTC().Format(time.RFC3339),
		})
	}
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{"count": len(items), "acknowledgements": items})
}

func (h *Handler) finalizeContext(r *http.Request) (context.Context, context.CancelFunc) {
	timeout := h.FinalizeTimeout
	if timeout <= 0 {
		timeout = defaultFinalizeTimeout
	}
	return context.WithTimeout(context.WithoutCancel(r.Context()), timeout)
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

// refuse counts a refused request and records the denial.
func (h *Handler) refuse(ctx context.Context, reasonCode string, authCtx auth.AuthContext, correlationID string) {
	h.count(ctx, outcomeRefused)
	h.record(ctx, governanceaudit.DecisionDenied, reasonCode, authCtx, correlationID)
}

// record appends one governance audit event for a decision. It never blocks the
// action and carries only the bounded reason code, the actor class, and hashes:
// never the operator's reason, the instance, or the scope.
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

// ackFingerprint is the stable, non-sensitive fingerprint of an acknowledgement
// target, so a reused idempotency key with a different target is rejected rather
// than treated as a duplicate.
func ackFingerprint(kind, instance, scope string) string {
	sum := sha256.Sum256([]byte("collector_failure_ack\x00" + kind + "\x00" + instance + "\x00" + scope))
	return hex.EncodeToString(sum[:])
}
