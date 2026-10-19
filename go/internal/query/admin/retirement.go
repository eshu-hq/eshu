// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// RetirementFencer runs the phase-1 mark-and-fence for the trigger's
// repositories. pgstatus.RetirementFencer satisfies it; the handler keeps
// the interface so tests script the fence without a database.
type RetirementFencer interface {
	FenceRepository(context.Context, pgstatus.FenceRequest) (pgstatus.FenceResult, error)
}

// RetirementPurger purges the trigger's fenced repositories. A blocked
// repository lands in PurgeResult.Blocked; only a malformed request, a
// wrong-phase tombstone, or a failed transaction returns an error.
// pgstatus.RetirementPurger satisfies it.
type RetirementPurger interface {
	PurgeRetired(context.Context, pgstatus.PurgeRequest) (pgstatus.PurgeResult, error)
}

// retireRequest is the operator trigger request body. repositories,
// reason, and idempotency_key are mandatory.
type retireRequest struct {
	Repositories   []string `json:"repositories"`
	Reason         string   `json:"reason"`
	IdempotencyKey string   `json:"idempotency_key"`
}

func (r *retireRequest) normalize() {
	for i, selector := range r.Repositories {
		r.Repositories[i] = strings.TrimSpace(selector)
	}
	r.Reason = strings.TrimSpace(r.Reason)
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
}

// resolvedRetirement is one selector resolved to the canonical repo and
// default-branch scope the fence marks, plus the minted tombstone key.
type resolvedRetirement struct {
	Selector     string
	RepoID       string
	ScopeID      string
	RetirementID string
}

// retireRepositories is the operator trigger for repository retirement
// (#8602): it resolves every repository selector, runs the phase-1
// mark-and-fence, then purges the fenced repositories, in that order. The
// fence owns mark-then-fence atomically (one transaction, shared with the
// standalone mark insert so the two writers cannot drift apart), so the
// trigger invokes fence then purge and never purges what the fence did not
// mark. A blocked fence never reaches the purge; an unfenced tombstone or a
// live generation refuses at the purge instead of purging partially. Every
// accepted or refused trigger records an admin_retirement_action audit
// event. POST /api/v0/admin/repositories/retire
func (h *Handler) retireRepositories(w http.ResponseWriter, r *http.Request) {
	if h.RetirementFencer == nil || h.RetirementPurger == nil || h.Repositories == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "repository retirement trigger not configured")
		return
	}

	var req retireRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.normalize()

	authCtx, _ := auth.AuthContextFromContext(r.Context())
	correlationID := audit.SafeCorrelationID(audit.CorrelationID(r))

	if len(req.Repositories) == 0 {
		h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_missing_repositories", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest, "repositories is required and must name at least one repository")
		return
	}
	if len(req.Repositories) > pgstatus.RetirementFenceMaxRepositories {
		h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_too_many_repositories", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest,
			fmt.Sprintf("at most %d repositories per request", pgstatus.RetirementFenceMaxRepositories))
		return
	}
	if req.Reason == "" {
		h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_missing_reason", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest, "reason is required and must explain why the retirement is safe")
		return
	}
	if req.IdempotencyKey == "" {
		h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_missing_idempotency_key", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusBadRequest, "idempotency_key is required to make retirement safe under retries")
		return
	}
	// Authorization gate, mirroring reopen: an admin/all-scopes principal
	// may retire. A request with no auth context (auth.Mode == "") is
	// unauthenticated dev mode, where every admin route is intentionally
	// open. A scoped or otherwise limited token is denied.
	if authCtx.Mode != "" && !authCtx.AllScopes {
		h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_unauthorized", authCtx, correlationID)
		querycontract.WriteError(w, http.StatusForbidden, "retire requires an admin (all-scopes) token")
		return
	}

	// Resolve before the fence: an unknown, ambiguous, or non-git selector
	// is an expected, side-effect-free refusal, and refusing it here marks
	// nothing. The first problem in selector order decides the status, so a
	// mixed request still refuses whole. Each refusal writes a literal
	// status at its own site (#7674 guard).
	resolved, refuse, resolveErr := h.resolveRetirements(r.Context(), req.Repositories)
	if resolveErr != nil {
		// A failure before anything was marked: nothing was retired and
		// the key is not consumed, so a 499 on a client cancel is exact.
		// A catalog read gets the inspection envelope; an id-minting
		// failure is a plain server fault.
		if errors.Is(resolveErr, errRetireIDMint) {
			writeMutationFailure(w, r, resolveErr, retireFailedMessage)
			return
		}
		writeInspectionFailure(w, r, resolveErr, repositoryCatalogLookupCapability, retireLookupFailedMessage)
		return
	}
	if refuse != nil {
		h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, refuse.auditCode, authCtx, correlationID)
		switch refuse.kind {
		case retireRefusalUnknownRepository:
			querycontract.WriteError(w, http.StatusNotFound, refuse.reason)
		case retireRefusalAmbiguousRepository:
			querycontract.WriteError(w, http.StatusConflict, refuse.reason)
		default:
			querycontract.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"status": "refused",
				"reason": refuse.reason,
				"detail": refuse.detail,
			})
		}
		return
	}

	actorClass, actorIDHash := audit.RecoveryActor(authCtx)
	fenceReq := pgstatus.FenceRequest{
		Retirements:        make([]pgstatus.FenceRetirement, 0, len(resolved)),
		ReasonCode:         pgstatus.RetirementReasonOperatorRetired,
		ReasonHash:         audit.IdentityHash(req.Reason),
		ActorClass:         string(actorClass),
		ActorIDHash:        actorIDHash,
		IdempotencyKeyHash: audit.IdentityHash(req.IdempotencyKey),
	}
	for _, res := range resolved {
		fenceReq.Retirements = append(fenceReq.Retirements, pgstatus.FenceRetirement{
			RetirementID: res.RetirementID,
			RepoID:       res.RepoID,
			ScopeID:      res.ScopeID,
		})
	}
	// The fence commits the tombstone rows and the generation marks in one
	// transaction, so an error return may already have committed: a 499 on
	// a client cancel is ambiguous, and a retry with the same idempotency
	// key resumes the open rows instead of opening second ones.
	fenced, err := h.RetirementFencer.FenceRepository(r.Context(), fenceReq)
	if err != nil {
		var blocked *pgstatus.FenceBlockedError
		switch {
		case errors.As(err, &blocked):
			h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_fence_blocked", authCtx, correlationID)
			querycontract.WriteError(w, http.StatusConflict, "retirement fence blocked: "+blocked.Error())
			return
		case errors.Is(err, pgstatus.ErrRetirementAlreadyOpen):
			h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_already_open", authCtx, correlationID)
			querycontract.WriteError(w, http.StatusConflict, "a retirement is already open for this repository under a different idempotency key")
			return
		}
		writeMutationFailure(w, r, err, retireFailedMessage)
		return
	}

	// The purge runs only after the fence committed, on the fence's sorted
	// repo ids: a resumed fence reports the existing rows, so the purge
	// covers exactly what is open. Each repository purges in its own
	// transaction; a commit-time error is ambiguous, never partial, and a
	// blocked repository keeps its fenced tombstone open for a later pass.
	purged, err := h.RetirementPurger.PurgeRetired(r.Context(), pgstatus.PurgeRequest{RepoIDs: fenced.RepoIDs})
	if err != nil {
		switch {
		case errors.Is(err, pgstatus.ErrRetirementNoOpenTombstone):
			h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_unfenced", authCtx, correlationID)
			querycontract.WriteError(w, http.StatusConflict, "retirement purge refused: no open tombstone for this repository")
			return
		case errors.Is(err, pgstatus.ErrRetirementTombstoneNotFenced):
			h.recordRetirementAction(r.Context(), governanceaudit.DecisionDenied, "retire_refused_wrong_phase", authCtx, correlationID)
			querycontract.WriteError(w, http.StatusConflict, "retirement purge refused: the tombstone is not pending/fenced")
			return
		}
		writeMutationFailure(w, r, err, retireFailedMessage)
		return
	}

	// The fence and the purge both committed: record the outcome on a
	// context a client cancel cannot abort, so the accepted audit event is
	// never lost to a client leaving (finalizeContext, mirroring reopen).
	finalizeCtx, cancelFinalize := h.finalizeContext(r)
	defer cancelFinalize()
	status := "retired"
	auditCode := "retire_accepted"
	if len(purged.Blocked) > 0 {
		// A blocked purge is the designed deferred exit, not a denial:
		// the tombstone stays fenced and open, and the operator retries
		// the same idempotency key after the generations go terminal.
		status = "blocked"
		auditCode = "retire_accepted_blocked"
	}
	h.recordRetirementAction(finalizeCtx, governanceaudit.DecisionAllowed, auditCode, authCtx, correlationID)
	slog.Default().InfoContext(finalizeCtx, "repository retirement trigger accepted",
		slog.String("operation", "retire"),
		slog.String("route", r.URL.Path),
		slog.String("correlation_id", correlationID),
		slog.Any("retirement_ids", orEmptyStrings(fenced.RetirementIDs)),
		slog.Any("repo_ids", orEmptyStrings(fenced.RepoIDs)),
		slog.Int("completed_count", len(purged.Completed)),
		slog.Int("blocked_count", len(purged.Blocked)),
		slog.String("status", status),
	)
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"status":             status,
		"repositories":       orEmptyStrings(req.Repositories),
		"retirement_ids":     orEmptyStrings(fenced.RetirementIDs),
		"repo_ids":           orEmptyStrings(fenced.RepoIDs),
		"scope_ids":          orEmptyStrings(fenced.ScopeIDs),
		"generations_fenced": fenced.GenerationsFenced,
		"completed":          orEmptyStrings(purged.Completed),
		"blocked":            purgeBlockedToSlice(purged.Blocked),
		"idempotency_key":    req.IdempotencyKey,
	})
}

// retireRefusalKind names one side-effect-free resolution refusal. The
// handler switches on the kind with a literal status per site, because the
// #7674 guard forbids struct fields as status operands.
type retireRefusalKind int

const (
	retireRefusalUnknownRepository retireRefusalKind = iota + 1
	retireRefusalAmbiguousRepository
	retireRefusalNotGitRepository
)

// retireRefusal is one side-effect-free resolution refusal: nothing was
// marked, so the idempotency key stays unconsumed.
type retireRefusal struct {
	kind      retireRefusalKind
	auditCode string
	reason    string
	detail    string
}

// errRetireIDMint marks a tombstone-key minting failure inside resolution,
// so the handler answers it as a server fault instead of a lookup failure.
var errRetireIDMint = errors.New("retirement id generation failed")

// resolveRetirements resolves every selector to one git default-branch
// repository and mints its tombstone key, sorted by repo id so retries mint
// in a stable order. Duplicate selectors for one repository collapse. The
// first problem in selector order refuses the whole request. A catalog
// lookup failure returns as an error for the inspection-failure envelope;
// an id-minting failure cannot happen after anything was marked, so it
// returns wrapped in errRetireIDMint for the server-fault envelope.
func (h *Handler) resolveRetirements(ctx context.Context, selectors []string) ([]resolvedRetirement, *retireRefusal, error) {
	byRepo := make(map[string]resolvedRetirement, len(selectors))
	for _, selector := range selectors {
		matches, err := h.Repositories.MatchRepositories(ctx, selector)
		if err != nil {
			// Nothing is marked before every selector resolves, so a
			// cancel here is exact.
			return nil, nil, err
		}
		entry, problem := gitDefaultScopeMatch(matches)
		if problem != "" {
			return nil, classifyRetireProblem(selector, problem), nil
		}
		if _, seen := byRepo[entry.ID]; seen {
			continue
		}
		retirementID, err := newRetirementID()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", errRetireIDMint, err)
		}
		byRepo[entry.ID] = resolvedRetirement{
			Selector:     selector,
			RepoID:       entry.ID,
			ScopeID:      entry.ScopeID,
			RetirementID: retirementID,
		}
	}
	resolved := make([]resolvedRetirement, 0, len(byRepo))
	for _, res := range byRepo {
		resolved = append(resolved, res)
	}
	slices.SortFunc(resolved, func(a, b resolvedRetirement) int {
		return strings.Compare(a.RepoID, b.RepoID)
	})
	return resolved, nil, nil
}

// classifyRetireProblem maps a catalog resolution problem to its refusal.
// Unknown selectors are 404, ambiguous ones 409, and selectors that resolve
// to a non-git scope are 422: only git default-branch repositories retire.
func classifyRetireProblem(selector, problem string) *retireRefusal {
	switch {
	case strings.HasPrefix(problem, "no repository matched"):
		return &retireRefusal{
			kind:      retireRefusalUnknownRepository,
			auditCode: "retire_refused_unknown_repository",
			reason:    fmt.Sprintf("no repository matches %q", selector),
		}
	case strings.HasPrefix(problem, "matched multiple repositories"):
		return &retireRefusal{
			kind:      retireRefusalAmbiguousRepository,
			auditCode: "retire_refused_ambiguous_repository",
			reason:    fmt.Sprintf("%s for %q", problem, selector),
		}
	default:
		return &retireRefusal{
			kind:      retireRefusalNotGitRepository,
			auditCode: "retire_refused_not_git_repository",
			reason:    problem,
			detail:    "only git default-branch repositories retire; other scopes fail closed",
		}
	}
}

// newRetirementID mints a tombstone key with the rr_ prefix.
func newRetirementID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate retirement id: %w", err)
	}
	return "rr_" + hex.EncodeToString(buf[:]), nil
}

// purgeBlockedToSlice renders the designed deferred exits for the response.
func purgeBlockedToSlice(blocked []pgstatus.PurgeBlocked) []any {
	out := make([]any, 0, len(blocked))
	for _, entry := range blocked {
		out = append(out, map[string]any{
			"repo_id":       entry.RepoID,
			"retirement_id": entry.RetirementID,
			"reason":        entry.Reason,
			"detail":        entry.Detail,
			"statuses":      entry.Statuses,
		})
	}
	return out
}

// recordRetirementAction records one admin_retirement_action governance
// audit event, denied or accepted. It mirrors recordRecoveryAction: the
// audit carries actor class, admin scope class, decision, and reason code,
// and no repository, scope, or payload identifiers.
func (h *Handler) recordRetirementAction(
	ctx context.Context,
	decision governanceaudit.Decision,
	reasonCode string,
	authCtx auth.AuthContext,
	correlationID string,
) {
	if h.Audit == nil {
		return
	}
	actorClass, actorIDHash := audit.RecoveryActor(authCtx)
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeAdminRetirementAction,
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
