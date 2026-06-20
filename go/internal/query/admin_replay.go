package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// sharedAdminActorIDHash is a stable, non-sensitive actor identity for shared
// admin-token recovery actions, which carry no per-subject hash. It lets the
// governance audit record a shared_token actor without inventing or leaking a
// real identity.
var sharedAdminActorIDHash = func() string {
	sum := sha256.Sum256([]byte("eshu:shared-admin-token"))
	return "sha256:" + hex.EncodeToString(sum[:])
}()

// replayRequest is the admin replay request body. reason and idempotency_key
// are mandatory; force opts past the unsafe-class guard.
type replayRequest struct {
	WorkItemIDs    []string `json:"work_item_ids"`
	ScopeID        string   `json:"scope_id"`
	Stage          string   `json:"stage"`
	FailureClass   string   `json:"failure_class"`
	OperatorNote   string   `json:"operator_note"`
	Reason         string   `json:"reason"`
	IdempotencyKey string   `json:"idempotency_key"`
	Force          bool     `json:"force"`
	Limit          int      `json:"limit"`
}

// replay safely replays terminally failed fact-projection work items. It
// requires an explicit reason and idempotency key, gates on admin authorization,
// refuses unsafe failure classes, records a governance audit event, and dedupes
// concurrent or duplicate delivery through the admin_replay_requests ledger.
// POST /api/v0/admin/replay
func (h *AdminHandler) replay(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req replayRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.normalize()

	auth, _ := AuthContextFromContext(r.Context())
	correlationID := safeAuditCorrelationID(documentationCorrelationID(r))

	if !req.hasSelector() {
		WriteError(w, http.StatusBadRequest,
			"at least one selector is required: work_item_ids, scope_id, stage, or failure_class")
		return
	}
	if req.Reason == "" {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "replay_refused_missing_reason", auth, correlationID)
		WriteError(w, http.StatusBadRequest, "reason is required and must explain why the replay is safe")
		return
	}
	if req.IdempotencyKey == "" {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "replay_refused_missing_idempotency_key", auth, correlationID)
		WriteError(w, http.StatusBadRequest, "idempotency_key is required to make replay safe under retries")
		return
	}
	// Authorization gate (explicit allow-list): an admin/all-scopes principal may
	// replay. A request with no auth context (auth.Mode == "") is unauthenticated
	// dev mode, where every admin route is intentionally open, consistent with
	// dead-letter/skip/refinalize. A scoped or otherwise limited token is denied.
	if auth.Mode != "" && !auth.AllScopes {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "replay_refused_unauthorized", auth, correlationID)
		WriteError(w, http.StatusForbidden, "replay requires an admin (all-scopes) token")
		return
	}
	// Refuse an explicit unsafe failure-class target unless forced.
	if guidance, unsafe := unsafeReplayRefusal(req.FailureClass); unsafe && !req.Force {
		h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "replay_refused_unsafe_class", auth, correlationID)
		WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"status":        "refused",
			"failure_class": req.FailureClass,
			"reason":        guidance,
			"detail":        "set force=true to replay this class after addressing the cause",
		})
		return
	}

	fingerprint := replayRequestFingerprint(req.WorkItemIDs, req.ScopeID, req.Stage, req.FailureClass, req.limit(), req.Force)
	claim, err := h.Store.ClaimReplayIdempotency(r.Context(), req.IdempotencyKey, fingerprint, h.now())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("replay idempotency: %v", err))
		return
	}
	if !claim.Claimed {
		h.respondDuplicateReplay(w, r.Context(), req, claim, fingerprint, auth, correlationID)
		return
	}

	exclude := []string(nil)
	if !req.Force {
		exclude = unsafeReplayFailureClassList()
	}
	items, err := h.Store.ReplayFailedWorkItems(r.Context(), ReplayWorkItemFilter{
		WorkItemIDs:           req.WorkItemIDs,
		ScopeID:               req.ScopeID,
		Stage:                 req.Stage,
		FailureClass:          req.FailureClass,
		OperatorNote:          req.noteForReplay(),
		Limit:                 req.limit(),
		ExcludeFailureClasses: exclude,
	})
	if err != nil {
		// The replay UPDATE and its replay-event insert are not a single atomic
		// unit, so an error here may have already moved work items to pending.
		// Leave the ledger row in_progress rather than abandoning it: deleting it
		// would let a retry with the same key claim a fresh row, observe zero
		// terminal items, and lose the prior outcome. A retry instead gets a
		// 409 so an operator inspects what was replayed.
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("replay: %v", err))
		return
	}

	ids := workItemIDList(items)
	if err := h.Store.CompleteReplayIdempotency(r.Context(), req.IdempotencyKey, len(items), ids, h.now()); err != nil {
		// The replay already committed; only the ledger finalize failed. Leave the
		// row in_progress (retry returns 409) so the durable outcome is never
		// erased by a reclaim.
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("replay record: %v", err))
		return
	}

	h.recordRecoveryAction(r.Context(), governanceaudit.DecisionAllowed, "replay_accepted", auth, correlationID)
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":          "replayed",
		"replayed_count":  len(items),
		"work_item_ids":   ids,
		"replayed":        workItemsToSlice(items),
		"idempotency_key": req.IdempotencyKey,
		"duplicate":       false,
	})
}

// respondDuplicateReplay handles a replay request whose idempotency key already
// exists: a completed key returns the prior outcome, an in-progress key is told
// to wait, and a key reused with different selectors is rejected.
func (h *AdminHandler) respondDuplicateReplay(
	w http.ResponseWriter,
	ctx context.Context,
	req replayRequest,
	claim ReplayIdempotencyClaim,
	fingerprint string,
	auth AuthContext,
	correlationID string,
) {
	if claim.Fingerprint != "" && claim.Fingerprint != fingerprint {
		h.recordRecoveryAction(ctx, governanceaudit.DecisionDenied, "replay_idempotency_key_reused", auth, correlationID)
		WriteError(w, http.StatusConflict, "idempotency_key was already used with different replay parameters")
		return
	}
	// An empty status means the ledger row vanished between the conflicting
	// claim and the read (e.g. a concurrent prune). Fail closed and ask the
	// caller to retry rather than report a false duplicate success.
	if claim.Status != replayRequestStatusCompleted {
		WriteError(w, http.StatusConflict, "a replay for this idempotency_key is already in progress")
		return
	}
	h.recordRecoveryAction(ctx, governanceaudit.DecisionAllowed, "replay_idempotent_replay", auth, correlationID)
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":          "replayed",
		"replayed_count":  claim.ReplayedCount,
		"work_item_ids":   claim.WorkItemIDs,
		"idempotency_key": req.IdempotencyKey,
		"duplicate":       true,
	})
}

// recordRecoveryAction appends one governance audit event for a replay decision.
// It never blocks the action and never carries raw identifiers or secrets.
func (h *AdminHandler) recordRecoveryAction(
	ctx context.Context,
	decision governanceaudit.Decision,
	reasonCode string,
	auth AuthContext,
	correlationID string,
) {
	if h.Audit == nil {
		return
	}
	actorClass, actorIDHash := adminRecoveryActor(auth)
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeAdminRecoveryAction,
		ActorClass:         actorClass,
		ActorIDHash:        actorIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           decision,
		ReasonCode:         reasonCode,
		CorrelationID:      correlationID,
		PolicyRevisionHash: auth.PolicyRevisionHash,
		OccurredAt:         h.now(),
	}
	appendCtx, cancel := context.WithTimeout(ctx, governanceAuditAppendTimeout)
	defer cancel()
	_ = h.Audit.Append(appendCtx, []governanceaudit.Event{event})
}

// adminRecoveryActor maps an auth context to a governance audit actor class and
// identity hash. A shared admin token carries no per-subject hash, so it uses a
// stable synthetic identity rather than an empty one.
func adminRecoveryActor(auth AuthContext) (governanceaudit.ActorClass, string) {
	if auth.Mode == AuthModeScoped {
		if auth.SubjectIDHash == "" {
			return governanceaudit.ActorClassAnonymous, ""
		}
		return governanceaudit.ActorClassScopedToken, auth.SubjectIDHash
	}
	if auth.SubjectIDHash != "" {
		return governanceaudit.ActorClassSharedToken, auth.SubjectIDHash
	}
	return governanceaudit.ActorClassSharedToken, sharedAdminActorIDHash
}

func (r *replayRequest) normalize() {
	r.ScopeID = strings.TrimSpace(r.ScopeID)
	r.Stage = strings.TrimSpace(r.Stage)
	r.FailureClass = strings.TrimSpace(r.FailureClass)
	r.OperatorNote = strings.TrimSpace(r.OperatorNote)
	r.Reason = strings.TrimSpace(r.Reason)
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
}

func (r replayRequest) hasSelector() bool {
	return len(r.WorkItemIDs) > 0 || r.ScopeID != "" || r.Stage != "" || r.FailureClass != ""
}

// noteForReplay persists the operator-facing why on each replay event. The
// explicit reason is used when no separate operator note is supplied.
func (r replayRequest) noteForReplay() string {
	if r.OperatorNote != "" {
		return r.OperatorNote
	}
	return r.Reason
}

func (r replayRequest) limit() int {
	if r.Limit <= 0 {
		return 100
	}
	return r.Limit
}

func workItemIDList(items []AdminWorkItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.WorkItemID)
	}
	return ids
}
