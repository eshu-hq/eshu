// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Trigger outcomes. Accepted marks the committed tombstone; exactly one
// terminal outcome (completed, blocked, refused, failed) follows per
// repository, so the eshu_dp_repository_retirements_total funnel adds up.
const (
	// TriggerOutcomeAccepted counts a committed tombstone mark.
	TriggerOutcomeAccepted = "accepted"
	// TriggerOutcomeRefused counts a mark conflict, a validation refusal,
	// or a fence refusal: nothing was purged.
	TriggerOutcomeRefused = "refused"
	// TriggerOutcomeBlocked counts a designed purge exit that wrote
	// nothing: the fenced tombstone stays open for a later pass.
	TriggerOutcomeBlocked = "blocked"
	// TriggerOutcomeCompleted counts a purged and tombstoned repository.
	TriggerOutcomeCompleted = "completed"
	// TriggerOutcomeFailed counts a trigger error: the repository reached
	// no designed exit.
	TriggerOutcomeFailed = "failed"
)

// Trigger audit reason codes for the admin_retirement_action event. The
// fence and purge reasons are the bounded foundation constants, so the audit
// trail names the exact refusal without carrying raw identifiers.
const (
	triggerReasonAccepted = "repository_retire_accepted"
	triggerReasonRefused  = "repository_retire_refused"
	triggerReasonBlocked  = "repository_retire_blocked"
	triggerReasonComplete = "repository_retire_completed"
	triggerReasonFailed   = "repository_retire_failed"
)

// Marker opens tombstone rows. RetirementStore satisfies it.
type Marker interface {
	InsertPending(context.Context, pgstorage.RetirementRequest) (pgstorage.RetirementRecord, error)
}

// Fencer runs the phase-1 mark-and-fence. RetirementFencer satisfies it.
type Fencer interface {
	FenceRepository(context.Context, pgstorage.FenceRequest) (pgstorage.FenceResult, error)
}

// Purger runs the narrowed purge. RetirementPurger satisfies it.
type Purger interface {
	PurgeRetired(context.Context, pgstorage.PurgeRequest) (pgstorage.PurgeResult, error)
}

// TriggerRetirement is one repository's trigger entry: the server-generated
// tombstone key, the canonical repo id, and the default-branch scope.
type TriggerRetirement struct {
	RetirementID string
	RepoID       string
	ScopeID      string
}

// TriggerRequest is the operator's retire input. Reason and IdempotencyKey
// are raw operator values: the trigger hashes them before they reach the
// store, so plaintext never lands in a tombstone row, a log, or an audit
// event. ActorClass and ActorIDHash arrive already mapped from the request's
// auth context by the caller.
type TriggerRequest struct {
	Retirements    []TriggerRetirement
	Reason         string
	ActorClass     string
	ActorIDHash    string
	IdempotencyKey string
}

// RepoOutcome is one repository's trigger outcome.
type RepoOutcome struct {
	RetirementID string
	RepoID       string
	ScopeID      string
	// Outcome is one of the TriggerOutcome* values.
	Outcome string
	// BlockedReason, BlockedDetail, and BlockedStatuses carry the purge's
	// designed exit (terminal_not_reached or scope_lock_busy); empty
	// otherwise.
	BlockedReason   string
	BlockedDetail   string
	BlockedStatuses string
}

// TriggerResult reports the request's per-repository outcomes in request
// order.
type TriggerResult struct {
	Repos []RepoOutcome
	// Duplicate reports a same-key retry that resumed an open retirement
	// instead of opening a new one.
	Duplicate bool
}

// Trigger runs mark, fence, and purge in the designed order (#8602). It is
// the single production caller of RetirementFencer and RetirementPurger.
type Trigger struct {
	Marker Marker
	Fencer Fencer
	Purger Purger
	// Instruments carries the trigger outcome counter. Nil disables
	// emission; the trigger itself is unaffected.
	Instruments *telemetry.Instruments
	// Audit records admin_retirement_action events. Nil disables audit
	// recording; the trigger itself proceeds.
	Audit audit.Appender
	// Logger defaults to the process default logger.
	Logger *slog.Logger
}

func (t *Trigger) logger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}

// TriggerRetirement marks every repository, fences the marked set, then
// purges the fenced set. A mark error returns before any fence or purge; a
// fence refusal returns before any purge; a designed purge exit (blocked)
// lands on the result without an error. Only a malformed request, a
// wrong-phase tombstone, or a failed step returns an error.
func (t *Trigger) TriggerRetirement(ctx context.Context, req TriggerRequest) (TriggerResult, error) {
	if err := validateTriggerRequest(req); err != nil {
		t.countRefused(ctx, repoIDsOf(req.Retirements))
		return TriggerResult{}, err
	}
	repoIDs := repoIDsOf(req.Retirements)
	reasonHash := audit.IdentityHash(req.Reason)
	keyHash := audit.IdentityHash(req.IdempotencyKey)
	duplicate := false
	for _, retirement := range req.Retirements {
		record, err := t.Marker.InsertPending(ctx, pgstorage.RetirementRequest{
			RetirementID:       retirement.RetirementID,
			RepoID:             retirement.RepoID,
			ScopeID:            retirement.ScopeID,
			ReasonCode:         pgstorage.RetirementReasonOperatorRetired,
			ReasonHash:         reasonHash,
			ActorClass:         req.ActorClass,
			ActorIDHash:        req.ActorIDHash,
			IdempotencyKeyHash: keyHash,
		})
		if err != nil {
			if errors.Is(err, pgstorage.ErrRetirementAlreadyOpen) {
				t.countRefused(ctx, repoIDs)
				t.audit(ctx, req, retirement, governanceaudit.DecisionDenied, triggerReasonRefused)
			} else {
				t.countOutcome(ctx, TriggerOutcomeFailed, repoIDs)
			}
			return TriggerResult{}, err
		}
		if record.RetirementID != retirement.RetirementID {
			duplicate = true
		}
		t.countOutcome(ctx, TriggerOutcomeAccepted, []string{retirement.RepoID})
		t.audit(ctx, req, retirement, governanceaudit.DecisionAllowed, triggerReasonAccepted)
	}
	fenceReq := pgstorage.FenceRequest{
		ReasonCode:         pgstorage.RetirementReasonOperatorRetired,
		ReasonHash:         reasonHash,
		ActorClass:         req.ActorClass,
		ActorIDHash:        req.ActorIDHash,
		IdempotencyKeyHash: keyHash,
	}
	for _, retirement := range req.Retirements {
		fenceReq.Retirements = append(fenceReq.Retirements, pgstorage.FenceRetirement{
			RetirementID: retirement.RetirementID,
			RepoID:       retirement.RepoID,
			ScopeID:      retirement.ScopeID,
		})
	}
	fenceResult, err := t.Fencer.FenceRepository(ctx, fenceReq)
	if err != nil {
		t.countRefused(ctx, repoIDs)
		for _, retirement := range req.Retirements {
			t.audit(ctx, req, retirement, governanceaudit.DecisionDenied, triggerReasonRefused)
		}
		t.logger().WarnContext(ctx, "repository retirement refused at fence",
			slog.String("outcome", TriggerOutcomeRefused),
			slog.Any("repo_ids", repoIDs),
			slog.String("error", err.Error()),
		)
		return TriggerResult{Repos: refusedOutcomes(req.Retirements), Duplicate: duplicate}, err
	}
	fencedIDs := make(map[string]string, len(fenceResult.RetirementIDs))
	for i, repoID := range fenceResult.RepoIDs {
		if i < len(fenceResult.RetirementIDs) {
			fencedIDs[repoID] = fenceResult.RetirementIDs[i]
		}
	}
	purgeResult, err := t.Purger.PurgeRetired(ctx, pgstorage.PurgeRequest{RepoIDs: repoIDs})
	if err != nil {
		t.countOutcome(ctx, TriggerOutcomeFailed, repoIDs)
		for _, retirement := range req.Retirements {
			t.audit(ctx, req, retirement, governanceaudit.DecisionDenied, triggerReasonFailed)
		}
		return TriggerResult{Repos: failedOutcomes(req.Retirements), Duplicate: duplicate}, err
	}
	blockedByRepo := make(map[string]pgstorage.PurgeBlocked, len(purgeResult.Blocked))
	for _, blocked := range purgeResult.Blocked {
		blockedByRepo[blocked.RepoID] = blocked
	}
	completed := make(map[string]struct{}, len(purgeResult.Completed))
	for _, repoID := range purgeResult.Completed {
		completed[repoID] = struct{}{}
	}
	outcomes := make([]RepoOutcome, 0, len(req.Retirements))
	for _, retirement := range req.Retirements {
		id := retirement.RetirementID
		if fenced, ok := fencedIDs[retirement.RepoID]; ok {
			id = fenced
		}
		outcome := RepoOutcome{RetirementID: id, RepoID: retirement.RepoID, ScopeID: retirement.ScopeID}
		switch {
		case isCompleted(completed, retirement.RepoID):
			outcome.Outcome = TriggerOutcomeCompleted
			t.countOutcome(ctx, TriggerOutcomeCompleted, []string{retirement.RepoID})
			t.audit(ctx, req, retirement, governanceaudit.DecisionAllowed, triggerReasonComplete)
		default:
			blocked := blockedByRepo[retirement.RepoID]
			outcome.Outcome = TriggerOutcomeBlocked
			outcome.BlockedReason = blocked.Reason
			outcome.BlockedDetail = blocked.Detail
			outcome.BlockedStatuses = blocked.Statuses
			t.countOutcome(ctx, TriggerOutcomeBlocked, []string{retirement.RepoID})
			t.audit(ctx, req, retirement, governanceaudit.DecisionDenied, triggerReasonBlocked)
		}
		outcomes = append(outcomes, outcome)
	}
	return TriggerResult{Repos: outcomes, Duplicate: duplicate}, nil
}

// validateTriggerRequest checks the operator input shape before any store
// call: 1 to 25 repositories (the fence and purge request cap), every id
// present, no duplicate repository, and the mandatory reason and
// idempotency key.
func validateTriggerRequest(req TriggerRequest) error {
	if len(req.Retirements) == 0 || len(req.Retirements) > pgstorage.RetirementFenceMaxRepositories {
		return fmt.Errorf("retirement trigger: want 1 to %d repositories, got %d",
			pgstorage.RetirementFenceMaxRepositories, len(req.Retirements))
	}
	if strings.TrimSpace(req.Reason) == "" {
		return fmt.Errorf("retirement trigger: reason is required and must explain why the retirement is safe")
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return fmt.Errorf("retirement trigger: idempotency_key is required to make retirement safe under retries")
	}
	seen := make(map[string]struct{}, len(req.Retirements))
	for _, retirement := range req.Retirements {
		if strings.TrimSpace(retirement.RetirementID) == "" {
			return fmt.Errorf("retirement trigger: empty retirement id")
		}
		if strings.TrimSpace(retirement.RepoID) == "" {
			return fmt.Errorf("retirement trigger: empty repo id")
		}
		if strings.TrimSpace(retirement.ScopeID) == "" {
			return fmt.Errorf("retirement trigger: empty scope id")
		}
		if _, dup := seen[retirement.RepoID]; dup {
			return fmt.Errorf("retirement trigger: duplicate repo id %q", retirement.RepoID)
		}
		seen[retirement.RepoID] = struct{}{}
	}
	return nil
}

// repoIDsOf returns the request's repository ids, sorted, for the purge
// request and per-request outcome counting.
func repoIDsOf(retirements []TriggerRetirement) []string {
	repoIDs := make([]string, 0, len(retirements))
	for _, retirement := range retirements {
		repoIDs = append(repoIDs, retirement.RepoID)
	}
	sort.Strings(repoIDs)
	return repoIDs
}

// refusedOutcomes reports every repository refused when the fence stops the
// request before any purge.
func refusedOutcomes(retirements []TriggerRetirement) []RepoOutcome {
	outcomes := make([]RepoOutcome, 0, len(retirements))
	for _, retirement := range retirements {
		outcomes = append(outcomes, RepoOutcome{
			RetirementID: retirement.RetirementID, RepoID: retirement.RepoID,
			ScopeID: retirement.ScopeID, Outcome: TriggerOutcomeRefused,
		})
	}
	return outcomes
}

// failedOutcomes reports every repository failed when the purge errors.
func failedOutcomes(retirements []TriggerRetirement) []RepoOutcome {
	outcomes := make([]RepoOutcome, 0, len(retirements))
	for _, retirement := range retirements {
		outcomes = append(outcomes, RepoOutcome{
			RetirementID: retirement.RetirementID, RepoID: retirement.RepoID,
			ScopeID: retirement.ScopeID, Outcome: TriggerOutcomeFailed,
		})
	}
	return outcomes
}

func isCompleted(completed map[string]struct{}, repoID string) bool {
	_, ok := completed[repoID]
	return ok
}

// countOutcome moves the trigger outcome counter once per repository. Nil
// instruments are a no-op.
func (t *Trigger) countOutcome(ctx context.Context, outcome string, repoIDs []string) {
	if t.Instruments == nil {
		return
	}
	for range repoIDs {
		t.Instruments.OperatorRepositoryRetirements.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrOutcome(outcome)),
		)
	}
}

// countRefused counts a fence or mark refusal once per repository.
func (t *Trigger) countRefused(ctx context.Context, repoIDs []string) {
	t.countOutcome(ctx, TriggerOutcomeRefused, repoIDs)
}

// audit appends one admin_retirement_action event. It never blocks the
// trigger and never carries raw identifiers: the scope travels as a hash,
// the reason as the bounded reason code. A nil appender disables audit
// recording.
func (t *Trigger) audit(
	ctx context.Context,
	req TriggerRequest,
	retirement TriggerRetirement,
	decision governanceaudit.Decision,
	reasonCode string,
) {
	if t.Audit == nil {
		return
	}
	event := governanceaudit.Event{
		Type:          governanceaudit.EventTypeAdminRetirementAction,
		ActorClass:    governanceaudit.ActorClass(req.ActorClass),
		ActorIDHash:   req.ActorIDHash,
		ScopeClass:    governanceaudit.ScopeClassRepository,
		ScopeIDHash:   audit.IdentityHash(retirement.ScopeID),
		Decision:      decision,
		ReasonCode:    reasonCode,
		CorrelationID: audit.IdentityHash(req.IdempotencyKey),
		OccurredAt:    time.Now().UTC(),
	}
	appendCtx, cancel := context.WithTimeout(ctx, audit.AppendTimeout)
	defer cancel()
	_ = t.Audit.Append(appendCtx, []governanceaudit.Event{event})
}
