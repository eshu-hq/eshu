// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Reopenable domains for POST /api/v0/admin/reopen: the three domains the
// #7285 ops-qa repair reopened by hand. The strings mirror the canonical
// reducercontract domain names without importing the reducer tree (see the
// package AGENTS.md import direction); a dedicated test pins them.
const (
	// ReopenDomainRepoDependency reopens completed shared-projection intents,
	// one row per acceptance unit.
	ReopenDomainRepoDependency = "repo_dependency"
	// ReopenDomainWorkloadMaterialization reopens succeeded reducer rows.
	ReopenDomainWorkloadMaterialization = "workload_materialization"
	// ReopenDomainSubmodulePin reopens succeeded reducer rows.
	ReopenDomainSubmodulePin = "submodule_pin"
)

// DefaultReopenLimit bounds one reopen call when the request names no limit.
// The handler and the store share it so ReopenFilter's documented default
// holds in both layers.
const DefaultReopenLimit = 1000

// Reopen errors the store returns for expected operator-facing failures. The
// handler maps them to statuses; anything else is a 500 that leaves the
// idempotency claim in progress.
var (
	// ErrReopenScopeNotFound means no ingestion scope matches the selector.
	ErrReopenScopeNotFound = errors.New("reopen scope not found")
	// ErrReopenNoActiveGeneration means the scope has no resolvable active
	// generation to reopen work for.
	ErrReopenNoActiveGeneration = errors.New("reopen scope has no active generation")
)

// ReopenFilter selects completed work to reopen.
type ReopenFilter struct {
	// Domain is one of the ReopenDomain* constants, validated by the handler.
	Domain string
	// ScopeID is the ingestion scope id or source key.
	ScopeID string
	// Limit bounds the reopened rows. Values <= 0 select the default.
	Limit int
}

// ReopenedUnit records one acceptance unit re-driven by an intent reopen:
// the unit, the accepted source run the store resolved, and the single
// intent row reopened to trigger the unit's cycle.
type ReopenedUnit struct {
	AcceptanceUnitID string
	SourceRunID      string
	IntentID         string
}

// ReopenResult is the outcome of a reopen: the resolved generation plus the
// reopened row identities. Exactly one of the two id lists is populated,
// depending on the domain kind.
type ReopenResult struct {
	GenerationID       string
	ReducerWorkItemIDs []string
	IntentIDs          []string
	Units              []ReopenedUnit
}

// reopenRequest is the admin reopen request body. domain, scope_id, reason
// and idempotency_key are mandatory.
type reopenRequest struct {
	Domain         string `json:"domain"`
	ScopeID        string `json:"scope_id"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
	Limit          int    `json:"limit"`
}

// reopen reopens completed reducer or shared-projection work for one domain
// and scope, the admin surface for the #7285 hand-SQL repair. It requires an
// explicit domain, scope, reason and idempotency key, gates on admin
// authorization, records a governance audit event, and dedupes concurrent or
// duplicate delivery through the admin_replay_requests ledger, shared with
// replay (the fingerprint discriminator keeps the two operations apart).
// POST /api/v0/admin/reopen
func (h *Handler) reopen(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req reopenRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.normalize()

	authCtx, _ := auth.AuthContextFromContext(r.Context())
	correlationID := audit.SafeCorrelationID(audit.CorrelationID(r))

	if !isReopenDomain(req.Domain) {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "reopen_refused_unknown_domain", authCtx, correlationID)
		querycontract.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"status": "refused",
			"domain": req.Domain,
			"reason": "unknown reopen domain: want one of repo_dependency, workload_materialization, submodule_pin",
			"detail": "other domains fail closed; reopening them needs a repair-sized review first",
		})
		return
	}
	if req.ScopeID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id is required")
		return
	}
	if req.Reason == "" {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "reopen_refused_missing_reason", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest, "reason is required and must explain why the reopen is safe")
		return
	}
	if req.IdempotencyKey == "" {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "reopen_refused_missing_idempotency_key", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest, "idempotency_key is required to make reopen safe under retries")
		return
	}
	// Authorization gate (explicit allow-list), mirroring replay: an
	// admin/all-scopes principal may reopen. A request with no auth context
	// (auth.Mode == "") is unauthenticated dev mode, where every admin route
	// is intentionally open. A scoped or otherwise limited token is denied.
	if authCtx.Mode != "" && !authCtx.AllScopes {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "reopen_refused_unauthorized", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusForbidden, "reopen requires an admin (all-scopes) token")
		return
	}

	// Resolve before the claim: an unknown scope or a generation-less scope
	// is an expected, side-effect-free refusal, and refusing it here leaves
	// the idempotency key unconsumed for a corrected retry. The store
	// re-resolves authoritatively at run time inside ReopenCompletedWork.
	if _, _, err := h.Store.ResolveReopenTarget(r.Context(), req.ScopeID); err != nil {
		switch {
		case errors.Is(err, ErrReopenScopeNotFound):
			h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "reopen_refused_unknown_scope", authCtx, correlationID)
			querycontract.WriteError(w, http.StatusNotFound, "no ingestion scope matches scope_id")
			return
		case errors.Is(err, ErrReopenNoActiveGeneration):
			h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "reopen_refused_no_active_generation", authCtx, correlationID)
			querycontract.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"status": "refused",
				"reason": "scope has no active generation to reopen work for",
				"detail": "nothing was reopened and the idempotency key was not consumed",
			})
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("reopen resolve: %v", err))
		return
	}

	fingerprint := reopenRequestFingerprint(req.Domain, req.ScopeID, req.limit())
	claim, err := h.Store.ClaimReplayIdempotency(r.Context(), req.IdempotencyKey, fingerprint, h.now())
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("reopen idempotency: %v", err))
		return
	}
	if !claim.Claimed {
		h.respondDuplicateReopen(w, r.Context(), req, claim, fingerprint, authCtx, correlationID)
		return
	}

	res, err := h.Store.ReopenCompletedWork(r.Context(), ReopenFilter{
		Domain:  req.Domain,
		ScopeID: req.ScopeID,
		Limit:   req.limit(),
	})
	if err != nil {
		// The store runs the reopen in one transaction with a deferred
		// rollback, so an error return means rolled back: only a
		// commit-time error is ambiguous, never partial. Failures map to
		// statuses while the ledger row stays in_progress, so a retry
		// gets a 409 instead of losing the outcome.
		// The scope/generation arms below only fire on a resolve race: the
		// pre-claim probe already refused the expected cases.
		switch {
		case errors.Is(err, ErrReopenScopeNotFound):
			h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "reopen_refused_unknown_scope", authCtx, correlationID)
			querycontract.WriteError(w, http.StatusNotFound, "no ingestion scope matches scope_id")
			return
		case errors.Is(err, ErrReopenNoActiveGeneration):
			h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "reopen_refused_no_active_generation", authCtx, correlationID)
			querycontract.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"status": "refused",
				"reason": "scope has no active generation to reopen work for",
				"detail": "the scope lost its active generation after the claim; the idempotency key stays in progress",
			})
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("reopen: %v", err))
		return
	}

	combined := append(append([]string{}, res.ReducerWorkItemIDs...), res.IntentIDs...)
	if err := h.Store.CompleteReplayIdempotency(r.Context(), req.IdempotencyKey, len(combined), combined, h.now()); err != nil {
		// The reopen already committed; only the ledger finalize failed.
		// Leave the row in_progress (retry returns 409) so the durable
		// outcome is never erased by a reclaim.
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("reopen record: %v", err))
		return
	}

	h.recordRecoveryAction(r.Context(), governanceaudit.DecisionAllowed, "reopen_accepted", authCtx, correlationID)
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"status":                 "reopened",
		"domain":                 req.Domain,
		"scope_id":               req.ScopeID,
		"generation_id":          res.GenerationID,
		"reopened_reducer_count": len(res.ReducerWorkItemIDs),
		"reopened_reducer_ids":   orEmptyStrings(res.ReducerWorkItemIDs),
		"reopened_intent_count":  len(res.IntentIDs),
		"reopened_intent_ids":    orEmptyStrings(res.IntentIDs),
		"reopened_units":         reopenedUnitsToSlice(res.Units),
		"reopened_total_count":   len(combined),
		"idempotency_key":        req.IdempotencyKey,
		"duplicate":              false,
	})
}

// respondDuplicateReopen handles a reopen request whose idempotency key
// already exists: a completed key returns the prior outcome, an in-progress
// key is told to wait, and a key reused with different selectors is rejected.
// The ledger stores one combined id list, so a duplicate reports totals
// without the reducer/intent split the first completion carried.
func (h *Handler) respondDuplicateReopen(
	w http.ResponseWriter,
	ctx context.Context,
	req reopenRequest,
	claim ReplayIdempotencyClaim,
	fingerprint string,
	authCtx auth.AuthContext,
	correlationID string,
) {
	if claim.Fingerprint != "" && claim.Fingerprint != fingerprint {
		h.recordRecoveryAction(ctx, governanceaudit.DecisionDenied, "reopen_idempotency_key_reused", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusConflict, "idempotency_key was already used with different reopen parameters")
		return
	}
	// An empty status means the ledger row vanished between the conflicting
	// claim and the read (e.g. a concurrent prune). Fail closed and ask the
	// caller to retry rather than report a false duplicate success.
	if claim.Status != ReplayRequestStatusCompleted {
		querycontract.WriteError(w, http.StatusConflict, "a reopen for this idempotency_key is already in progress")
		return
	}
	h.recordRecoveryAction(ctx, governanceaudit.DecisionAllowed, "reopen_idempotent_replay", authCtx, correlationID)
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"status":               "reopened",
		"reopened_total_count": claim.ReplayedCount,
		"reopened_ids":         orEmptyStrings(claim.WorkItemIDs),
		"idempotency_key":      req.IdempotencyKey,
		"duplicate":            true,
	})
}

// isReopenDomain reports whether the domain is one of the three reopenable
// domains. Every other domain fails closed.
func isReopenDomain(domain string) bool {
	switch domain {
	case ReopenDomainRepoDependency, ReopenDomainWorkloadMaterialization, ReopenDomainSubmodulePin:
		return true
	default:
		return false
	}
}

// reopenRequestFingerprint derives a stable, non-sensitive fingerprint of a
// reopen request. The "reopen" discriminator keeps it apart from replay
// fingerprints sharing the admin_replay_requests ledger, so a key used for a
// replay can never silently validate as a reopen with the same scope.
func reopenRequestFingerprint(domain, scopeID string, limit int) string {
	var builder strings.Builder
	builder.WriteString("operation=reopen")
	builder.WriteString("\ndomain=")
	builder.WriteString(strings.TrimSpace(domain))
	builder.WriteString("\nscope=")
	builder.WriteString(strings.TrimSpace(scopeID))
	fmt.Fprintf(&builder, "\nlimit=%d", limit)

	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func (r *reopenRequest) normalize() {
	r.Domain = strings.TrimSpace(r.Domain)
	r.ScopeID = strings.TrimSpace(r.ScopeID)
	r.Reason = strings.TrimSpace(r.Reason)
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
}

func (r reopenRequest) limit() int {
	if r.Limit <= 0 {
		return DefaultReopenLimit
	}
	return r.Limit
}

func orEmptyStrings(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func reopenedUnitsToSlice(units []ReopenedUnit) []any {
	out := make([]any, 0, len(units))
	for _, unit := range units {
		out = append(out, map[string]any{
			"acceptance_unit_id": unit.AcceptanceUnitID,
			"source_run_id":      unit.SourceRunID,
			"intent_id":          unit.IntentID,
		})
	}
	return out
}
