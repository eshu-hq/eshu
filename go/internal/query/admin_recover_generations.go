// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// recoverGenerationsRequest is the operator recovery request body. It re-drives
// wedged scopes through reduce -> readiness -> projection over existing facts
// without a full re-clone. reason and idempotency_key are mandatory so the
// action is auditable and safe under retries and concurrent delivery.
//
// The request names either a scope set or, with all_scopes, the whole
// deployment. all_scopes is the disaster-recovery mode: rebuilding the graph
// from preserved Postgres facts after a restore, when the operator has no scope
// list to type. See the DR runbook in
// docs/public/operate/graph-rebuild-from-facts.md.
type recoverGenerationsRequest struct {
	ScopeIDs       []string `json:"scope_ids"`
	AllScopes      bool     `json:"all_scopes"`
	Reason         string   `json:"reason"`
	IdempotencyKey string   `json:"idempotency_key"`
}

func (r *recoverGenerationsRequest) normalize() {
	r.Reason = strings.TrimSpace(r.Reason)
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	scopeIDs := make([]string, 0, len(r.ScopeIDs))
	for _, id := range r.ScopeIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			scopeIDs = append(scopeIDs, trimmed)
		}
	}
	r.ScopeIDs = scopeIDs
}

// recoverGenerations is the operator escape hatch for wedged generations. It
// durably re-enqueues projector work for the named scopes (the same durable Go
// work queue refinalize uses, so the re-drive re-publishes
// canonical-nodes-committed and re-triggers downstream reducer consumers), and
// records the action in the admin_replay_requests ledger through the
// idempotency claim/complete pair so the ledger no longer stays empty for
// recovery work. Unlike reindex, no work is lost: the durable enqueue is the
// unit of recovery, and the ledger is the durable record of it.
//
// POST /api/v0/admin/recover-generations
func (h *AdminHandler) recoverGenerations(w http.ResponseWriter, r *http.Request) {
	if h.Recovery == nil {
		WriteError(w, http.StatusServiceUnavailable, "recovery handler not configured")
		return
	}
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req recoverGenerationsRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.normalize()

	auth, _ := AuthContextFromContext(r.Context())
	correlationID := safeAuditCorrelationID(documentationCorrelationID(r))

	if req.AllScopes && len(req.ScopeIDs) > 0 {
		WriteError(w, http.StatusBadRequest, "all_scopes cannot be combined with scope_ids: rebuild every active scope or name a list, not both")
		return
	}
	if !req.AllScopes && len(req.ScopeIDs) == 0 {
		WriteError(w, http.StatusBadRequest, "scope_ids is required and must name at least one wedged scope, or set all_scopes to rebuild every active scope from preserved facts")
		return
	}
	if req.Reason == "" {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "recover_generations_refused_missing_reason", auth, correlationID)
		WriteError(w, http.StatusBadRequest, "reason is required and must explain why the recovery is safe")
		return
	}
	if req.IdempotencyKey == "" {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "recover_generations_refused_missing_idempotency_key", auth, correlationID)
		WriteError(w, http.StatusBadRequest, "idempotency_key is required to make recovery safe under retries")
		return
	}
	// Authorization gate mirrors replay: an admin/all-scopes principal may
	// recover; unauthenticated dev mode (auth.Mode == "") is intentionally open;
	// a scoped token is denied.
	if auth.Mode != "" && !auth.AllScopes {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "recover_generations_refused_unauthorized", auth, correlationID)
		WriteError(w, http.StatusForbidden, "recover-generations requires an admin (all-scopes) token")
		return
	}

	fingerprint := recoverGenerationsFingerprint(req.ScopeIDs, req.AllScopes)
	claim, err := h.Store.ClaimReplayIdempotency(r.Context(), req.IdempotencyKey, fingerprint, h.now())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "recover-generations idempotency: "+err.Error())
		return
	}
	if !claim.Claimed {
		h.respondDuplicateRecoverGenerations(w, r, req, claim, fingerprint, auth, correlationID)
		return
	}

	result, err := h.Recovery.Refinalize(r.Context(), recovery.RefinalizeFilter{
		ScopeIDs:  req.ScopeIDs,
		AllScopes: req.AllScopes,
	})
	if err != nil {
		// The durable enqueue failed before completion; leave the ledger row
		// in_progress so a retry surfaces a 409 rather than re-driving blindly.
		WriteError(w, http.StatusInternalServerError, "recover-generations: "+err.Error())
		return
	}

	if err := h.Store.CompleteReplayIdempotency(r.Context(), req.IdempotencyKey, result.Enqueued, result.ScopeIDs, h.now()); err != nil {
		WriteError(w, http.StatusInternalServerError, "recover-generations record: "+err.Error())
		return
	}

	h.recordRecoveryAction(r.Context(), governanceaudit.DecisionAllowed, "recover_generations_accepted", auth, correlationID)
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":          "recovered",
		"enqueued":        result.Enqueued,
		"scope_ids":       result.ScopeIDs,
		"idempotency_key": req.IdempotencyKey,
		"duplicate":       false,
	})
}

// respondDuplicateRecoverGenerations returns the prior outcome for a key that
// already completed, rejects a key reused with different scopes, and asks the
// caller to retry while a recovery is still in progress.
func (h *AdminHandler) respondDuplicateRecoverGenerations(
	w http.ResponseWriter,
	r *http.Request,
	req recoverGenerationsRequest,
	claim ReplayIdempotencyClaim,
	fingerprint string,
	auth AuthContext,
	correlationID string,
) {
	if claim.Fingerprint != "" && claim.Fingerprint != fingerprint {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "recover_generations_idempotency_key_reused", auth, correlationID)
		// The fingerprint covers both the scope list and the all_scopes flag, so
		// this also fires when a key from a scoped recovery is reused for a
		// whole-deployment rebuild. Name both, or an operator reads "different
		// scope_ids" on a request that carried none.
		WriteError(w, http.StatusConflict, "idempotency_key was already used with a different scope_ids list or all_scopes setting")
		return
	}
	if claim.Status != replayRequestStatusCompleted {
		WriteError(w, http.StatusConflict, "a recovery for this idempotency_key is already in progress")
		return
	}
	h.recordRecoveryAction(r.Context(), governanceaudit.DecisionAllowed, "recover_generations_idempotent_replay", auth, correlationID)
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":          "recovered",
		"enqueued":        claim.ReplayedCount,
		"scope_ids":       claim.WorkItemIDs,
		"idempotency_key": req.IdempotencyKey,
		"duplicate":       true,
	})
}

// recoverGenerationsFingerprint derives a stable, non-sensitive fingerprint of a
// recovery request from its scope set so a reused idempotency key with a
// different scope set is rejected rather than silently treated as a duplicate.
//
// allScopes changes the stage component rather than riding in the (empty) scope
// list, so an all-scopes rebuild and a scoped recovery can never collide on one
// fingerprint. Without that split, a key that already recovered two scopes would
// answer a whole-deployment rebuild with the two-scope outcome and report
// success while the rest of the graph stayed unbuilt.
func recoverGenerationsFingerprint(scopeIDs []string, allScopes bool) string {
	stage := "recover_generations"
	if allScopes {
		stage = "recover_generations_all_scopes"
	}
	return replayRequestFingerprint(scopeIDs, "", stage, "", 0, false)
}
