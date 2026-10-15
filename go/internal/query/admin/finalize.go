// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// defaultFinalizeTimeout bounds the idempotency finalize and the accepted
// audit append that run after a ledger-backed mutation committed, when
// Handler.FinalizeTimeout is unset.
const defaultFinalizeTimeout = 2 * time.Second

// finalizeAfterClientCancelEvent is the event_name of the INFO log a
// ledger-backed mutation writes when it recorded its outcome after the client
// had already canceled the request.
const finalizeAfterClientCancelEvent = "admin_recovery.finalize_after_client_cancel"

// finalizeContext returns the context the post-commit steps of a replay,
// reopen, or recover-generations request run on: the idempotency finalize
// and the accepted audit append (#7674). Once the mutation committed, a
// client cancel must not abort them, or the admin_replay_requests row stays
// in_progress and every retry with the key answers 409, with no expiry. The
// context keeps the request's values (the request span, auth, correlation)
// and drops only its cancel, bounded by Handler.FinalizeTimeout. The finalize
// is one autocommit UPDATE by primary key, so no lock or transaction outlives
// the statement and detaching it serializes nothing.
func (h *Handler) finalizeContext(r *http.Request) (context.Context, context.CancelFunc) {
	timeout := h.FinalizeTimeout
	if timeout <= 0 {
		timeout = defaultFinalizeTimeout
	}
	return context.WithTimeout(context.WithoutCancel(r.Context()), timeout)
}

// writeFinalizeFailure answers a failed idempotency finalize with message.
// It classifies on finalizeCtx, which never carries the request's
// context.Canceled, so the answer is always a server fault: 500 with err
// recorded on the request span. The mutation already applied; the ledger row
// stays in_progress.
func writeFinalizeFailure(w http.ResponseWriter, r *http.Request, finalizeCtx context.Context, err error, message string) {
	tracing.WriteServerFailure(w, r.WithContext(finalizeCtx), err, http.StatusInternalServerError, message)
}

// logFinalizeAfterClientCancel writes one INFO line when the outcome was
// recorded although the client had already left, so an operator can see that
// a 200 nobody received still applied and a retry will return it. It carries
// the route and the sanitized correlation id, no identifiers or error text.
func logFinalizeAfterClientCancel(r *http.Request, finalizeCtx context.Context, operation, correlationID string) {
	if r.Context().Err() == nil {
		return
	}
	slog.Default().LogAttrs(finalizeCtx, slog.LevelInfo, "admin recovery outcome recorded after client cancel",
		telemetry.EventAttr(finalizeAfterClientCancelEvent),
		slog.String("operation", operation),
		slog.String("route", r.URL.Path),
		slog.String("correlation_id", correlationID),
	)
}
