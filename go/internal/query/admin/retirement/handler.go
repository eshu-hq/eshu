// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// maxRetirementSelectors bounds one retire request. It matches the fence
// and purge request caps so one operator request keeps one shape end to
// end.
const maxRetirementSelectors = pgstorage.RetirementFenceMaxRepositories

// Fixed client-facing messages for the retire route (#7674). The response
// body is exactly this text: a store error quotes SQL, relation names,
// hosts, and connection settings, so err.Error() never reaches a 5xx body.
// The cause is recorded on the request span instead.
const (
	retirementLookupFailedMessage  = "repository retirement selector lookup failed"
	retirementRequestFailedMessage = "repository retirement request failed"
	// retirementAlreadyOpenMessage answers a mark conflict: a retirement
	// is already open for the repository under a different idempotency
	// key. The per-repository entry identifies which repository (#8614).
	retirementAlreadyOpenMessage = "repository retirement refused: a retirement is already open for this repository under a different idempotency key"
)

// retirementCatalogLookupCapability names the repository catalog lookup in
// the reader verdict envelope. It reuses the registered reindex lookup key:
// the read is the same catalog match, and no new capability-matrix row is
// needed.
const retirementCatalogLookupCapability = "operator.repository_catalog.lookup"

// Catalog resolves repository selectors. The query ContentReader satisfies
// it.
type Catalog interface {
	MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error)
}

// Triggerer runs the operator retire sequence. Trigger satisfies it.
type Triggerer interface {
	TriggerRetirement(ctx context.Context, req TriggerRequest) (TriggerResult, error)
}

// Handler serves the operator retirement trigger route.
type Handler struct {
	// Catalog resolves selectors; nil makes the route 503.
	Catalog Catalog
	// Triggerer runs mark, fence, and purge; nil makes the route 503.
	Triggerer Triggerer
	// Audit records governance audit events for retire decisions. A nil
	// appender disables audit recording (the trigger still proceeds).
	Audit audit.Appender
	// Clock supplies the current time for audit events; nil uses
	// time.Now().UTC().
	Clock func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now().UTC()
}

// Mount registers the retirement trigger route on the given mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/admin/repository-retirements", h.retireRepository)
}

// retireRequest is the operator's retire body: 1 to 25 repository
// selectors plus the mandatory reason and idempotency key.
type retireRequest struct {
	Repositories   []string `json:"repositories"`
	Reason         string   `json:"reason"`
	IdempotencyKey string   `json:"idempotency_key"`
}

// retireRepository resolves every selector, then triggers mark, fence, and
// purge in order. It resolves and validates everything before the trigger
// runs, so a 400 answers before any tombstone row opens.
func (h *Handler) retireRepository(w http.ResponseWriter, r *http.Request) {
	if h.Catalog == nil || h.Triggerer == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "repository retirement handler not configured")
		return
	}
	var req retireRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	for i := range req.Repositories {
		req.Repositories[i] = strings.TrimSpace(req.Repositories[i])
	}
	req.Reason = strings.TrimSpace(req.Reason)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	authCtx, _ := auth.AuthContextFromContext(r.Context())
	if len(req.Repositories) == 0 || len(req.Repositories) > maxRetirementSelectors {
		h.recordDenial(r.Context(), "repository_retire_refused_invalid_request", authCtx)
		querycontract.WriteError(w, http.StatusBadRequest,
			fmt.Sprintf("no retirement recorded; want 1 to %d repositories, got %d",
				maxRetirementSelectors, len(req.Repositories)))
		return
	}
	if req.Reason == "" {
		h.recordDenial(r.Context(), "repository_retire_refused_missing_reason", authCtx)
		querycontract.WriteError(w, http.StatusBadRequest, "reason is required and must explain why the retirement is safe")
		return
	}
	if req.IdempotencyKey == "" {
		h.recordDenial(r.Context(), "repository_retire_refused_missing_idempotency_key", authCtx)
		querycontract.WriteError(w, http.StatusBadRequest, "idempotency_key is required to make retirement safe under retries")
		return
	}
	// Authorization gate mirrors replay and recover-generations: an
	// admin/all-scopes principal may retire; unauthenticated dev mode
	// (auth.Mode == "") is intentionally open; a scoped token is denied.
	if authCtx.Mode != "" && !authCtx.AllScopes {
		h.recordDenial(r.Context(), "repository_retire_refused_unauthorized", authCtx)
		querycontract.WriteError(w, http.StatusForbidden, "repository retirement requires an admin (all-scopes) token")
		return
	}
	retirements, problems, lookupFailed := h.resolveRepositories(w, r, req.Repositories)
	if lookupFailed {
		// The lookup failure already answered (reader-verdict 503 or
		// fixed-text 500); writing again would split the response.
		return
	}
	if len(problems) > 0 {
		h.recordDenial(r.Context(), "repository_retire_refused_selector", authCtx)
		querycontract.WriteError(w, http.StatusBadRequest,
			"no retirement recorded; every repository must resolve to one git repository: "+strings.Join(problems, "; "))
		return
	}
	actorClass, actorIDHash := audit.RecoveryActor(authCtx)
	triggerReq := TriggerRequest{
		Reason: req.Reason, ActorClass: string(actorClass),
		ActorIDHash: actorIDHash, IdempotencyKey: req.IdempotencyKey,
	}
	for _, retirement := range retirements {
		id, err := newRetirementID()
		if err != nil {
			tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, retirementRequestFailedMessage)
			return
		}
		triggerReq.Retirements = append(triggerReq.Retirements, TriggerRetirement{
			RetirementID: id, RepoID: retirement.RepoID, ScopeID: retirement.ScopeID,
		})
	}
	// The trigger commits each step in its own bounded transaction: the
	// mark, the fence attempt, and one purge transaction per repository. A
	// cancel observed after a step committed answers 499 with that step's
	// work durable, and a re-POST with the same idempotency key resumes the
	// open row instead of opening a second retirement.
	result, err := h.Triggerer.TriggerRetirement(r.Context(), triggerReq)
	if err != nil {
		var blocked *pgstorage.FenceBlockedError
		switch {
		case errors.As(err, &blocked):
			querycontract.WriteJSON(w, http.StatusConflict,
				triggerFailureBody(http.StatusConflict, result, req.IdempotencyKey,
					"repository retirement refused at fence: "+fenceRefusalDetail(blocked)))
			return
		case errors.Is(err, pgstorage.ErrRetirementAlreadyOpen):
			querycontract.WriteJSON(w, http.StatusConflict,
				triggerFailureBody(http.StatusConflict, result, req.IdempotencyKey, retirementAlreadyOpenMessage))
			return
		}
		// The envelope helper marks the request span and maps a client
		// cancel to 499; the body carries the fixed detail plus the
		// trigger's per-repository entries (#8614), never backend text.
		status, _ := tracing.ServerFailureEnvelope(r.Context(), err, retirementRequestFailedMessage, "")
		querycontract.WriteJSON(w, status,
			triggerFailureBody(status, result, req.IdempotencyKey, retirementRequestFailedMessage))
		return
	}
	querycontract.WriteJSON(w, http.StatusAccepted, triggerResponse(result, req.IdempotencyKey))
}

// resolvedRetirement is one selector's resolved default-branch scope.
type resolvedRetirement struct {
	RepoID  string
	ScopeID string
}

// resolveRepositories resolves every selector to exactly one git
// default-branch scope. It records nothing: the trigger runs only after
// every selector resolves, so the request stays all or nothing.
func (h *Handler) resolveRepositories(w http.ResponseWriter, r *http.Request, selectors []string) ([]resolvedRetirement, []string, bool) {
	repoIDByScope := make(map[string]string)
	var problems []string
	for _, selector := range selectors {
		matches, err := h.Catalog.MatchRepositories(r.Context(), selector)
		if err != nil {
			// Nothing is marked before every selector resolves, so a
			// cancel here is exact.
			writeRetirementLookupFailure(w, r, err)
			return nil, nil, true
		}
		entry, problem := defaultScopeMatch(matches)
		if problem != "" {
			problems = append(problems, fmt.Sprintf("%q: %s", selector, problem))
			continue
		}
		if _, seen := repoIDByScope[entry.ScopeID]; !seen {
			repoIDByScope[entry.ScopeID] = entry.ID
		}
	}
	if len(problems) > 0 {
		return nil, problems, false
	}
	resolved := make([]resolvedRetirement, 0, len(repoIDByScope))
	for scopeID, repoID := range repoIDByScope {
		resolved = append(resolved, resolvedRetirement{RepoID: repoID, ScopeID: scopeID})
	}
	slices.SortFunc(resolved, func(a, b resolvedRetirement) int {
		return strings.Compare(a.RepoID, b.RepoID)
	})
	return resolved, nil, false
}

// defaultScopeMatch returns the single catalog match when it is a git
// default-branch scope, or the reason the selector cannot be retired. A
// scope id disambiguates the duplicate-repository case.
func defaultScopeMatch(matches []querycontract.RepositoryCatalogEntry) (querycontract.RepositoryCatalogEntry, string) {
	switch len(matches) {
	case 0:
		return querycontract.RepositoryCatalogEntry{}, "no repository matched"
	case 1:
	default:
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		slices.Sort(ids)
		return querycontract.RepositoryCatalogEntry{}, "matched multiple repositories: " + strings.Join(ids, ", ")
	}
	entry := matches[0]
	if !recovery.IsGitDefaultBranchScope(entry.ScopeID) {
		return querycontract.RepositoryCatalogEntry{}, fmt.Sprintf("not a git default-branch repository (scope %q)", entry.ScopeID)
	}
	return entry, ""
}

// writeRetirementLookupFailure answers a failed catalog lookup: the shared
// retryable 503 verdict under the registered catalog key, else a 500 with
// the fixed message and the cause on the request span.
func writeRetirementLookupFailure(w http.ResponseWriter, r *http.Request, err error) {
	if querycontract.WriteGraphReadError(w, r, err, retirementCatalogLookupCapability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, retirementLookupFailedMessage)
}

// fenceRefusalDetail renders the bounded 409 reason. An over-budget
// refusal names the summed generations and reducer rows so the operator
// knows how far over the request cap the request ran; every value is a
// bounded count, never backend error text.
func fenceRefusalDetail(blocked *pgstorage.FenceBlockedError) string {
	if blocked == nil {
		return "fence refused"
	}
	if blocked.Reason == pgstorage.FenceReasonRequestTooLarge {
		return fmt.Sprintf("%s (%d generations, %d reducer rows over the request budget)",
			blocked.Reason, blocked.Generations, blocked.ReducerRows)
	}
	return blocked.Reason
}

// triggerResponse renders the 202 body: the per-repository outcomes inline,
// so a blocked purge needs no second read to name its retry.
func triggerResponse(result TriggerResult, idempotencyKey string) map[string]any {
	return map[string]any{
		"status": "accepted", "retirements": retirementEntries(result.Repos),
		"idempotency_key": idempotencyKey, "duplicate": result.Duplicate,
	}
}

// retirementEntries renders one entry per repository in request order,
// reusing the 202 entry shape for failure bodies: bounded ids plus bounded
// outcome strings, never backend error text.
func retirementEntries(repos []RepoOutcome) []map[string]any {
	entries := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		entry := map[string]any{
			"retirement_id": repo.RetirementID,
			"repository_id": repo.RepoID,
			"scope_id":      repo.ScopeID,
			"outcome":       repo.Outcome,
		}
		if repo.Outcome == TriggerOutcomeBlocked {
			entry["blocked_reason"] = repo.BlockedReason
			entry["blocked_detail"] = repo.BlockedDetail
			entry["blocking_statuses"] = repo.BlockedStatuses
		}
		entries = append(entries, entry)
	}
	return entries
}

// triggerFailureBody renders a trigger-failure body (#8614): the fixed
// detail (#7674) plus the trigger's per-repository entries, the idempotency
// key, and the duplicate flag, so a mid-mark failure still names every
// repository's outcome. Status codes and detail strings are unchanged.
func triggerFailureBody(status int, result TriggerResult, idempotencyKey, detail string) map[string]any {
	return map[string]any{
		"error": http.StatusText(status), "detail": detail,
		"retirements":     retirementEntries(result.Repos),
		"idempotency_key": idempotencyKey, "duplicate": result.Duplicate,
	}
}

// newRetirementID mints a server-side tombstone key. Same-key retries never
// reach this path twice for one row: the mark resumes the open row and the
// result reports the resumed id.
func newRetirementID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("retirement id: %w", err)
	}
	return "rr_" + hex.EncodeToString(raw[:]), nil
}

// recordDenial appends one denied admin_retirement_action event. It never
// blocks the response and never carries raw identifiers.
func (h *Handler) recordDenial(ctx context.Context, reasonCode string, authCtx auth.AuthContext) {
	if h.Audit == nil {
		return
	}
	actorClass, actorIDHash := audit.RecoveryActor(authCtx)
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeAdminRetirementAction,
		ActorClass:         actorClass,
		ActorIDHash:        actorIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           governanceaudit.DecisionDenied,
		ReasonCode:         reasonCode,
		PolicyRevisionHash: authCtx.PolicyRevisionHash,
		OccurredAt:         h.now(),
	}
	appendCtx, cancel := context.WithTimeout(ctx, audit.AppendTimeout)
	defer cancel()
	_ = h.Audit.Append(appendCtx, []governanceaudit.Event{event})
}
