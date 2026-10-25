// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The refusal details for the two places a store refusal can arrive. Before the
// ledger claim the key stays unconsumed. From the re-resolve inside the
// acknowledgement transaction the claim is already taken and the row stays in
// progress, so a retry with the same key answers 409.
//
// The 422 (no unresolved failure) carries the detail in its "detail" field. The
// 404 for an unknown instance has the same message on both paths; the
// post-claim body adds key_consumed and key_detail.
const (
	noFailureDetailUnclaimed = "nothing was acknowledged and the idempotency key was not consumed"
	noFailureDetailClaimed   = "nothing was acknowledged, the idempotency key is now consumed, and a retry needs a new idempotency_key"
	keyConsumedDetail        = noFailureDetailClaimed
	instanceNotFoundMessage  = "collector instance is not registered or not active for that collector_kind"
)

// answerStoreError maps a store error to its answer: the two refusals are
// client errors with their own audit reasons, and anything else is a server
// fault with the fixed body (#7674). claimed reports whether the idempotency
// claim was already taken: a refusal after it (a resolve race inside the
// acknowledgement transaction) leaves the ledger row in progress, so the 422
// and the 404 say the key is consumed instead of promising a same-key retry
// works.
func (h *Handler) answerStoreError(w http.ResponseWriter, r *http.Request, err error, claimed bool, authCtx auth.AuthContext, correlationID, scopeHash string) {
	switch {
	case errors.Is(err, pgstorage.ErrTerminalFailureAckInstanceNotFound):
		h.refuse(r.Context(), "ack_refused_instance_not_found", authCtx, correlationID, scopeHash)
		if claimed {
			// The instance was disabled or deactivated between the pre-claim
			// resolve and the in-transaction re-resolve. Keep the standard 404
			// body (error and detail) and add the consumed-key fields.
			querycontract.WriteJSON(w, http.StatusNotFound, map[string]any{
				"error":        http.StatusText(http.StatusNotFound),
				"detail":       instanceNotFoundMessage,
				"key_consumed": true,
				"key_detail":   keyConsumedDetail,
			})
			return
		}
		querycontract.WriteError(w, http.StatusNotFound, instanceNotFoundMessage)
	case errors.Is(err, pgstorage.ErrTerminalFailureAckNoFailure):
		h.refuse(r.Context(), "ack_refused_no_terminal_failure", authCtx, correlationID, scopeHash)
		detail := noFailureDetailUnclaimed
		if claimed {
			detail = noFailureDetailClaimed
		}
		querycontract.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"status": "refused",
			"reason": "no unresolved terminal failure inside the window on that instance and scope",
			"detail": detail,
		})
	default:
		h.record(r.Context(), governanceaudit.DecisionDenied, "ack_failed", authCtx, correlationID, scopeHash)
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, acknowledgeFailedMessage)
	}
}
